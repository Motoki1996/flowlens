//go:build integration

// BulkCreate's all-or-nothing rollback can only be verified against a real
// transaction — dbtest.FakeTxRunner (used by bulk_test.go) runs its closure
// directly against an in-memory fake with no rollback semantics at all, so
// it can't tell "committed" from "rolled back". Run with:
// make test-integration (requires DATABASE_URL and migrations applied).
package task_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/epic"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
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

// testProjectAndOwner creates a user and a project owned by it (with the
// owner's project_members row, via project.Service.Create — task.Service's
// authorize check needs that row, unlike the raw q.CreateProject some other
// packages' integration tests use), unique to this test run, deleted on
// cleanup.
func testProjectAndOwner(t *testing.T, pool *pgxpool.Pool) (db.User, project.Project) {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)

	suffix := time.Now().UnixNano()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Username:     fmt.Sprintf("task-bulk-%d", suffix),
		Email:        fmt.Sprintf("task-bulk-%d@example.com", suffix),
		DisplayName:  "Task Bulk Test",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
		assert.NoError(t, err)
	})

	projects := project.NewService(q)
	p, err := projects.Create(ctx, user.ID, "task-bulk-project", "")
	require.NoError(t, err)
	return user, p
}

func TestBulkCreate_AllOrNothing_RealPostgres(t *testing.T) {
	pool := testPool(t)
	user, p := testProjectAndOwner(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	projects := project.NewService(q)
	backlogs := backlog.NewService(q, database.NewTxRunner(pool), projects)
	epics := epic.NewService(q, database.NewTxRunner(pool), projects)
	svc := task.NewService(q, database.NewTxRunner(pool), projects, backlogs, epics)

	// Second task is invalid at the write step (title normalizes fine, but
	// the transaction should still fail cleanly): use a cyclic dependency
	// to force a mid-batch rollback after the first task has already been
	// inserted by createTaskInTx.
	_, err := svc.BulkCreate(ctx, user.ID, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "First"}},
			{Ref: "t2", CreateParams: task.CreateParams{Title: "Second"}},
		},
		Dependencies: []task.BulkDependencyParams{
			{PredecessorRef: "t1", SuccessorRef: "t2"},
			{PredecessorRef: "t2", SuccessorRef: "t1"},
		},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrBulkCyclicDependency)

	tasks, err := svc.List(ctx, user.ID, p.ID, task.ListFilter{})
	require.NoError(t, err)
	assert.Empty(t, tasks, "the whole batch must roll back, not just the rejected dependency")

	var depCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM task_dependencies td
		JOIN tasks t ON t.id = td.predecessor_task_id
		WHERE t.project_id = $1`, p.ID).Scan(&depCount))
	assert.Zero(t, depCount)
}

func TestBulkCreate_Commits_RealPostgres(t *testing.T) {
	pool := testPool(t)
	user, p := testProjectAndOwner(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	projects := project.NewService(q)
	backlogs := backlog.NewService(q, database.NewTxRunner(pool), projects)
	epics := epic.NewService(q, database.NewTxRunner(pool), projects)
	svc := task.NewService(q, database.NewTxRunner(pool), projects, backlogs, epics)

	result, err := svc.BulkCreate(ctx, user.ID, p.ID, task.BulkCreateParams{
		Tasks: []task.BulkTaskParams{
			{Ref: "t1", CreateParams: task.CreateParams{Title: "First"}},
			{Ref: "t2", CreateParams: task.CreateParams{Title: "Second"}},
		},
		Dependencies: []task.BulkDependencyParams{
			{PredecessorRef: "t1", SuccessorRef: "t2"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Tasks, 2)
	require.Len(t, result.Dependencies, 1)

	tasks, err := svc.List(ctx, user.ID, p.ID, task.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
}
