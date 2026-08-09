package sync_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustGetSyncJob(t *testing.T, q *dbtest.FakeQuerier, id uuid.UUID) db.SyncJob {
	t.Helper()
	job, ok := q.GetSyncJob(id)
	require.True(t, ok, "sync job %s not found", id)
	return job
}

func TestEnqueue_NoDedupeKey_AlwaysCreatesNewJob(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	_, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)
	_, err = syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	assert.Equal(t, 2, q.SyncJobCount())
}

func TestEnqueue_DedupeKeyCollidesWithPendingJob_ReusesRowInstead(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	first, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: project.ID,
		Kind:      "issue.update",
		Payload:   []byte(`{"title":"first"}`),
		DedupeKey: "issue.update:task-1",
	})
	require.NoError(t, err)

	second, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: project.ID,
		Kind:      "issue.update",
		Payload:   []byte(`{"title":"second"}`),
		DedupeKey: "issue.update:task-1",
	})
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "a colliding dedupe key must reuse the pending job, not create a new one")
	assert.Equal(t, 1, q.SyncJobCount())
	assert.JSONEq(t, `{"title":"second"}`, string(second.Payload), "reusing the job must refresh its payload to the latest edit")
}

func TestEnqueue_DedupeKeyCollidesWithTerminalJob_CreatesNewJob(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()
	registry := syncpkg.NewRegistry()
	registry.Register("issue.update", func(context.Context, db.SyncJob) error { return nil })
	worker := syncpkg.NewWorker(q, registry)

	first, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: project.ID,
		Kind:      "issue.update",
		DedupeKey: "issue.update:task-1",
	})
	require.NoError(t, err)

	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "succeeded", mustGetSyncJob(t, q, first.ID).Status)

	second, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: project.ID,
		Kind:      "issue.update",
		DedupeKey: "issue.update:task-1",
	})
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID, "a dedupe key must be reusable once the earlier job reached a terminal state")
	assert.Equal(t, "pending", second.Status)
	assert.Equal(t, 2, q.SyncJobCount())
}

func TestWorker_HandlerSucceeds_MarksJobSucceeded(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error { return nil })
	worker := syncpkg.NewWorker(q, registry)

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got := mustGetSyncJob(t, q, job.ID)
	assert.Equal(t, "succeeded", got.Status)
	assert.False(t, got.DedupeKey.Valid)
}

func TestWorker_HandlerFails_RetriesWithBackoffThenFails(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	wantErr := errors.New("gitlab: 500")
	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error { return wantErr })
	// baseBackoff 0 keeps run_after == now() after every retry, so the next
	// Poll call can reclaim the job immediately with no sleep.
	worker := syncpkg.NewWorker(q, registry, syncpkg.WithMaxAttempts(3), syncpkg.WithBaseBackoff(0))

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	for attempt := 1; attempt <= 2; attempt++ {
		n, err := worker.Poll(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n)

		got := mustGetSyncJob(t, q, job.ID)
		assert.Equal(t, "pending", got.Status, "attempt %d: a retry must go back to pending, not stay running", attempt)
		assert.Equal(t, int32(attempt), got.Attempts)
		assert.Equal(t, wantErr.Error(), got.LastError)
	}

	// Third and final attempt exceeds MaxAttempts(3).
	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got := mustGetSyncJob(t, q, job.ID)
	assert.Equal(t, "failed", got.Status)
	assert.Equal(t, int32(3), got.Attempts)
	assert.Equal(t, wantErr.Error(), got.LastError)
	assert.False(t, got.DedupeKey.Valid)
}

func TestWorker_HandlerFails_BackoffPushesRunAfterIntoTheFuture(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error { return errors.New("boom") })
	worker := syncpkg.NewWorker(q, registry, syncpkg.WithMaxAttempts(5), syncpkg.WithBaseBackoff(time.Hour))

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// A second poll immediately after must not reclaim the job: its run_after
	// was pushed roughly an hour into the future by the backoff.
	n, err = worker.Poll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a job whose run_after is in the future must not be claimed yet")

	got := mustGetSyncJob(t, q, job.ID)
	assert.True(t, got.RunAfter.Time.After(time.Now().Add(30*time.Minute)))
}

func TestWorker_NoHandlerRegistered_TreatedAsFailedAttempt(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	worker := syncpkg.NewWorker(q, syncpkg.NewRegistry(), syncpkg.WithMaxAttempts(5))

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "unregistered.kind"})
	require.NoError(t, err)

	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got := mustGetSyncJob(t, q, job.ID)
	assert.Equal(t, "pending", got.Status)
	assert.Equal(t, int32(1), got.Attempts)
	assert.Contains(t, got.LastError, "unregistered.kind")
}

func TestWorker_Poll_UpdatesProcessedCounterByKindAndOutcome(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error { return nil })
	registry.Register("issue.update", func(context.Context, db.SyncJob) error { return errors.New("boom") })
	worker := syncpkg.NewWorker(q, registry, syncpkg.WithMaxAttempts(1))

	succeededBefore := testutil.ToFloat64(syncpkg.JobsProcessedTotalForTest().WithLabelValues("issue.create", "succeeded"))
	failedBefore := testutil.ToFloat64(syncpkg.JobsProcessedTotalForTest().WithLabelValues("issue.update", "failed"))

	_, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)
	// MaxAttempts(1) means this job's single attempt already exhausts its
	// budget, landing straight on the "failed" outcome rather than "retry".
	_, err = syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.update"})
	require.NoError(t, err)

	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	assert.Equal(t, succeededBefore+1, testutil.ToFloat64(syncpkg.JobsProcessedTotalForTest().WithLabelValues("issue.create", "succeeded")))
	assert.Equal(t, failedBefore+1, testutil.ToFloat64(syncpkg.JobsProcessedTotalForTest().WithLabelValues("issue.update", "failed")))
}

func TestWorker_Poll_UpdatesProcessedCounterOnRetry(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error { return errors.New("boom") })
	worker := syncpkg.NewWorker(q, registry, syncpkg.WithMaxAttempts(5))

	retryBefore := testutil.ToFloat64(syncpkg.JobsProcessedTotalForTest().WithLabelValues("issue.create", "retry"))

	_, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	n, err := worker.Poll(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	assert.Equal(t, retryBefore+1, testutil.ToFloat64(syncpkg.JobsProcessedTotalForTest().WithLabelValues("issue.create", "retry")))
}

func TestWorker_Poll_UpdatesQueueDepthGauges(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	worker := syncpkg.NewWorker(q, syncpkg.NewRegistry())

	// No pending jobs yet: the gauges must read zero, not stay at whatever a
	// prior test in this binary left them at.
	_, err := worker.Poll(ctx)
	require.NoError(t, err)
	assert.Equal(t, float64(0), testutil.ToFloat64(syncpkg.PendingJobsGaugeForTest()))
	assert.Equal(t, float64(0), testutil.ToFloat64(syncpkg.OldestPendingJobAgeSecondsForTest()))

	// A job with no registered handler stays pending forever (it is never
	// claimed), so it is a stable way to keep the queue non-empty across the
	// second Poll below.
	q.SeedSyncJob(project.ID, "no.handler.registered", "pending", time.Now().Add(-time.Minute))
	worker = syncpkg.NewWorker(q, syncpkg.NewRegistry(), syncpkg.WithBatchSize(0))

	_, err = worker.Poll(ctx)
	require.NoError(t, err)
	assert.Equal(t, float64(1), testutil.ToFloat64(syncpkg.PendingJobsGaugeForTest()))
	assert.GreaterOrEqual(t, testutil.ToFloat64(syncpkg.OldestPendingJobAgeSecondsForTest()), 55.0, "the seeded job is ~1 minute old")
}

func TestWorker_ReclaimStale_RequeuesJobsOlderThanStaleAfter(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	stale := q.SeedSyncJob(project.ID, "issue.create", "running", time.Now().Add(-time.Hour))
	fresh := q.SeedSyncJob(project.ID, "issue.create", "running", time.Now())

	worker := syncpkg.NewWorker(q, syncpkg.NewRegistry(), syncpkg.WithStaleAfter(5*time.Minute))
	n, err := worker.ReclaimStale(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	assert.Equal(t, "pending", mustGetSyncJob(t, q, stale.ID).Status)
	assert.Equal(t, "running", mustGetSyncJob(t, q, fresh.ID).Status, "a recently-updated running job must not be reclaimed")
}

func TestWorker_Run_ReclaimsStaleJobsOnStartup(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	stale := q.SeedSyncJob(project.ID, "issue.create", "running", time.Now().Add(-time.Hour))

	worker := syncpkg.NewWorker(q, syncpkg.NewRegistry(), syncpkg.WithStaleAfter(5*time.Minute), syncpkg.WithPollInterval(time.Millisecond))
	runCtx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- worker.Run(runCtx) }()

	require.Eventually(t, func() bool {
		return mustGetSyncJob(t, q, stale.ID).Status == "pending"
	}, time.Second, time.Millisecond, "Run must reclaim stale jobs before its first poll")

	cancel()
	<-runDone
}

func TestWorker_Stop_WaitsForInFlightJobToFinishBeforeReturning(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error {
		close(handlerStarted)
		<-releaseHandler
		return nil
	})
	worker := syncpkg.NewWorker(q, registry, syncpkg.WithPollInterval(time.Millisecond))

	job, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	runDone := make(chan error, 1)
	go func() { runDone <- worker.Run(context.Background()) }()

	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("handler never started")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- worker.Stop(context.Background()) }()

	select {
	case <-stopDone:
		t.Fatal("Stop returned before the in-flight handler finished")
	case <-time.After(100 * time.Millisecond):
		// Expected: Stop is still blocked on the running job.
	}

	close(releaseHandler)

	select {
	case err := <-stopDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the handler finished")
	}
	<-runDone

	assert.Equal(t, "succeeded", mustGetSyncJob(t, q, job.ID).Status, "the in-flight job must complete normally, not be interrupted by shutdown")
}

func TestWorker_Stop_TimesOutIfHandlerNeverFinishes(t *testing.T) {
	q := dbtest.New()
	project := q.SeedProject(uuid.New(), "acme")
	ctx := context.Background()

	handlerStarted := make(chan struct{})
	block := make(chan struct{}) // never closed: simulates a handler that hangs past the shutdown deadline
	registry := syncpkg.NewRegistry()
	registry.Register("issue.create", func(context.Context, db.SyncJob) error {
		close(handlerStarted)
		<-block
		return nil
	})
	worker := syncpkg.NewWorker(q, registry, syncpkg.WithPollInterval(time.Millisecond))

	_, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{ProjectID: project.ID, Kind: "issue.create"})
	require.NoError(t, err)

	go func() { _ = worker.Run(context.Background()) }()
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("handler never started")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = worker.Stop(shutdownCtx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
