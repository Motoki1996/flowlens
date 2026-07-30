//go:build integration

// Integration tests exercise the outbox worker's hand-written SQL (ON
// CONFLICT dedupe, the FOR UPDATE SKIP LOCKED claim, the stale-running
// reclaim) against a real PostgreSQL instance — none of that can be verified
// against dbtest.FakeQuerier, which is why sync_test.go stays at the domain
// layer for everything else (retry counting, backoff math, dedupe policy,
// graceful shutdown). Run with: make test-integration
// (requires DATABASE_URL and migrations already applied).
package sync_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crossPackageLockKey serializes this package's integration tests against
// every other package that also drives real claim-and-execute workers over
// sync_jobs/webhook_events (internal/webhookapply, internal/synce2e): those
// tables have no per-package scoping, so `go test ./...`'s default
// parallelism across packages otherwise lets one package's worker claim rows
// enqueued by another's test, e.g. this file's own TestWorker_TwoWorkers...
// grabbing another package's outbound job (or vice versa) via the
// unscoped FOR UPDATE SKIP LOCKED claim. Must match the key used by those
// other packages' TestMain.
const crossPackageLockKey = 727208135

// TestMain holds a session-level Postgres advisory lock for this package's
// whole test run, so it and every other integration-tagged package sharing
// crossPackageLockKey run one at a time against the real database instead
// of racing. A session-level lock is released automatically if the process
// exits or the connection drops for any reason, so there is no deadlock risk
// from a crash or a CI-enforced timeout skipping the deferred unlock.
func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync: TestMain: connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", crossPackageLockKey); err != nil {
		fmt.Fprintf(os.Stderr, "sync: TestMain: acquire lock: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", crossPackageLockKey); err != nil {
		fmt.Fprintf(os.Stderr, "sync: TestMain: release lock: %v\n", err)
	}
	os.Exit(code)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := database.Connect(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// testProject creates a user and a project owned by it, unique to this test
// run, and deletes the user (cascading to the project and any sync_jobs rows
// it owns) on cleanup, per docs/testing.md's "keep integration tests
// independent" rule.
func testProject(t *testing.T, pool *pgxpool.Pool) db.Project {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)

	suffix := time.Now().UnixNano()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Username:     fmt.Sprintf("sync-worker-%d", suffix),
		Email:        fmt.Sprintf("sync-worker-%d@example.com", suffix),
		DisplayName:  "Sync Worker Test",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
		assert.NoError(t, err)
	})

	project, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: user.ID, Name: "sync-worker-project"})
	require.NoError(t, err)
	return project
}

func TestEnqueue_DedupeCollision_RealPostgres(t *testing.T) {
	pool := testPool(t)
	project := testProject(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	first, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: project.ID,
		Kind:      "issue.update",
		DedupeKey: fmt.Sprintf("issue.update:%s", uuid.NewString()),
	})
	require.NoError(t, err)

	second, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: project.ID,
		Kind:      "issue.update",
		DedupeKey: first.DedupeKey.String,
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "the real ON CONFLICT ... WHERE status = 'pending' clause must reuse the row")

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM sync_jobs WHERE dedupe_key = $1", first.DedupeKey.String).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestClaimPendingSyncJobs_RealPostgres_SkipsJobsNotYetDue(t *testing.T) {
	pool := testPool(t)
	project := testProject(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	// Push run_after into the future directly, the way a retry backoff would.
	err = q.MarkSyncJobRetry(ctx, db.MarkSyncJobRetryParams{
		ID:        job.ID,
		RunAfter:  pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		LastError: "not due yet",
	})
	require.NoError(t, err)

	claimed, err := q.ClaimPendingSyncJobs(ctx, 10)
	require.NoError(t, err)
	for _, c := range claimed {
		assert.NotEqual(t, job.ID, c.ID, "a job whose run_after is in the future must not be claimed by the real SQL")
	}
}

func TestWorker_TwoWorkersConcurrent_NoDoubleExecution(t *testing.T) {
	pool := testPool(t)
	project := testProject(t, pool)
	ctx := context.Background()

	const jobCount = 30
	jobIDs := make([]uuid.UUID, 0, jobCount)
	enqueueQ := db.New(pool)
	for i := 0; i < jobCount; i++ {
		job, err := syncpkg.Enqueue(ctx, enqueueQ, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
		require.NoError(t, err)
		jobIDs = append(jobIDs, job.ID)
	}

	var mu sync.Mutex
	executions := map[uuid.UUID]int{}
	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(_ context.Context, job db.SyncJob) error {
		// A brief pause widens the window in which two workers could
		// otherwise both grab the same row if SKIP LOCKED were not working.
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		executions[job.ID]++
		mu.Unlock()
		return nil
	})

	workerA := syncpkg.NewWorker(db.New(pool), registry, syncpkg.WithPollInterval(10*time.Millisecond), syncpkg.WithBatchSize(4))
	workerB := syncpkg.NewWorker(db.New(pool), registry, syncpkg.WithPollInterval(10*time.Millisecond), syncpkg.WithBatchSize(4))

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runWG sync.WaitGroup
	runWG.Add(2)
	go func() { defer runWG.Done(); _ = workerA.Run(runCtx) }()
	go func() { defer runWG.Done(); _ = workerB.Run(runCtx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(executions) == jobCount
	}, 10*time.Second, 20*time.Millisecond, "every enqueued job must eventually execute exactly once")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	require.NoError(t, workerA.Stop(shutdownCtx))
	require.NoError(t, workerB.Stop(shutdownCtx))
	cancel()
	runWG.Wait()

	mu.Lock()
	defer mu.Unlock()
	for _, id := range jobIDs {
		assert.Equal(t, 1, executions[id], "job %s must execute exactly once across both workers", id)
	}
}

func TestWorker_ReclaimStale_RealPostgres(t *testing.T) {
	pool := testPool(t)
	project := testProject(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	// Simulate a worker that claimed the job and then crashed: flip it to
	// 'running' with an updated_at far in the past.
	_, err = pool.Exec(ctx, "UPDATE sync_jobs SET status = 'running', updated_at = now() - interval '1 hour' WHERE id = $1", job.ID)
	require.NoError(t, err)

	worker := syncpkg.NewWorker(q, syncpkg.NewRegistry(), syncpkg.WithStaleAfter(5*time.Minute))
	n, err := worker.ReclaimStale(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	var status string
	require.NoError(t, pool.QueryRow(ctx, "SELECT status FROM sync_jobs WHERE id = $1", job.ID).Scan(&status))
	assert.Equal(t, "pending", status)
}
