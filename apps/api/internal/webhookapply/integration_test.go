//go:build integration

// Integration test exercises the inbound apply pipeline's guard 1
// (duplicate/concurrent create) against a real PostgreSQL instance: two
// goroutines applying two webhook_events for the same not-yet-known GitLab
// issue concurrently must produce exactly one task, enforced by the real
// UNIQUE (linked_gitlab_project_id, gitlab_issue_iid) constraint and the
// FOR UPDATE SKIP LOCKED claim — neither can be verified against
// dbtest.FakeQuerier (docs/testing.md's "keep it to the layer that owns the
// guarantee"). Run with: make test-integration (requires DATABASE_URL and
// migrations already applied).
package webhookapply_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/webhookapply"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// testLinkedProject creates a user, project, GitLab connection and linked
// GitLab project unique to this test run, and deletes the user (cascading
// to everything else) on cleanup, per docs/testing.md's "keep integration
// tests independent" rule.
func testLinkedProject(t *testing.T, pool *pgxpool.Pool) db.LinkedGitlabProject {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Username:     "webhookapply-" + suffix,
		Email:        "webhookapply-" + suffix + "@example.com",
		DisplayName:  "Webhook Apply Test",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
		assert.NoError(t, err)
	})

	project, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: user.ID, Name: "webhookapply-project"})
	require.NoError(t, err)

	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:      project.ID,
		BaseUrl:        "https://gitlab.example.com",
		EncryptedToken: []byte("fake-encrypted-token"),
	})
	require.NoError(t, err)

	link, err := q.CreateLinkedGitlabProject(ctx, db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    1,
		PathWithNamespace:  "group/project",
		Name:               "project",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)
	return link
}

func seedPendingIssueEvent(t *testing.T, pool *pgxpool.Pool, linkID db.LinkedGitlabProject, iid int64, title string) {
	t.Helper()
	payload := fmt.Sprintf(`{"object_kind":"issue","object_attributes":{"id":%d,"iid":%d,"title":%q,"state":"opened","updated_at":%q}}`,
		iid+1000, iid, title, time.Now().UTC().Format(time.RFC3339))
	q := db.New(pool)
	_, err := q.CreateWebhookEvent(context.Background(), db.CreateWebhookEventParams{
		LinkedGitlabProjectID: linkID.ID,
		DeliveryUuid:          fmt.Sprintf("%s-%d-%d", title, iid, time.Now().UnixNano()),
		EventName:             "Issue Hook",
		ObjectKind:            "issue",
		Payload:               []byte(payload),
		Status:                "pending",
	})
	require.NoError(t, err)
}

func TestProcessNext_ConcurrentEventsForSameNewIssue_OnlyOneTaskCreated_RealPostgres(t *testing.T) {
	pool := testPool(t)
	link := testLinkedProject(t, pool)

	const iid = int64(1)
	seedPendingIssueEvent(t, pool, link, iid, "delivery A")
	seedPendingIssueEvent(t, pool, link, iid, "delivery B")

	svc := webhookapply.NewService(database.NewTxRunner(pool))

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			// Retry: whichever goroutine loses the UNIQUE-constraint race
			// rolls its transaction back and must re-claim the same event on
			// its next attempt, this time taking the update path.
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				claimed, err := svc.ProcessNext(context.Background())
				if err != nil {
					t.Errorf("ProcessNext: %v", err)
					return
				}
				if !claimed {
					return
				}
			}
		}()
	}
	wg.Wait()

	var taskCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM task_gitlab_links WHERE linked_gitlab_project_id = $1 AND gitlab_issue_iid = $2`,
		link.ID, iid,
	).Scan(&taskCount))
	assert.Equal(t, 1, taskCount, "two concurrent deliveries for the same new issue must produce exactly one linked task")

	var processedCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM webhook_events WHERE linked_gitlab_project_id = $1 AND status = 'processed'`,
		link.ID,
	).Scan(&processedCount))
	assert.Equal(t, 2, processedCount, "both deliveries must eventually be applied (one create, one update)")
}
