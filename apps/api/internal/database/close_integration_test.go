//go:build integration

// The backlog/epic close columns (000036) against a live PostgreSQL.
//
// Two of this feature's promises are the database's, not the service's, and
// the in-memory fake (dbtest) hand-implements both — so a unit test passing
// against it proves the fake agrees with itself, not that the SQL is right:
//
//   - the status CHECK, which is what keeps 'archived' or a typo out of the
//     column whichever code path writes it;
//   - that Close/Reopen touch nothing but the two columns. There is no
//     trigger, no cascading FK and no second statement behind them, and this
//     is where that stays proven — the whole design turns on a backlog's close
//     saying nothing about the work filed inside it.
//
// Run with: make test-integration
package database_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Neither CreateBacklog nor CreateEpic names status, so the column default is
// what supplies it — which is also why every row predating 000036 reads as
// open with nothing to backfill.
func TestBacklogAndEpicDefaultToOpen(t *testing.T) {
	f := newEpicFixture(t, testDB(t))

	assert.Equal(t, "open", f.backlog.Status)
	assert.False(t, f.backlog.ClosedAt.Valid)

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	assert.Equal(t, "open", e.Status)
	assert.False(t, e.ClosedAt.Valid)
}

func TestBacklogAndEpicStatusCheckConstraint(t *testing.T) {
	pool := testPool(t)
	f := newEpicFixture(t, db.New(pool))
	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	ctx := context.Background()

	tests := []struct {
		name string
		stmt string
		id   any
	}{
		{"backlog", "UPDATE backlogs SET status = 'archived' WHERE id = $1", f.backlog.ID},
		{"epic", "UPDATE epics SET status = 'archived' WHERE id = $1", e.ID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.stmt, tt.id)
			require.Error(t, err, "status must be constrained to open/closed")
			assert.Contains(t, err.Error(), "status")
		})
	}
}

// The rule the whole design turns on: closing a backlog is a statement about
// the backlog. Its epics and its tasks are left exactly as they were.
func TestClosingABacklogLeavesItsEpicsAndTasksAlone(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	task := f.createTask(t, "Still open", &f.backlog.ID)
	_, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{
		EpicID: e.ID, TaskIds: []uuid.UUID{task.ID},
	})
	require.NoError(t, err)

	closed, err := f.q.CloseBacklogForOwner(ctx, db.CloseBacklogForOwnerParams{
		ID: f.backlog.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "closed", closed.Status)
	require.True(t, closed.ClosedAt.Valid)

	gotEpic, err := f.q.GetEpicForOwner(ctx, db.GetEpicForOwnerParams{ID: e.ID, OwnerUserID: f.owner.ID})
	require.NoError(t, err)
	assert.Equal(t, "open", gotEpic.Status, "closing a backlog must not close its epics")

	gotTask := f.taskByID(t, task.ID)
	assert.Equal(t, "open", gotTask.Status, "closing a backlog must not close its tasks")
	assert.False(t, gotTask.ClosedAt.Valid,
		"closing a backlog must not stamp a task's closed_at — internal/velocity reads it as a completion")
	assert.Equal(t, "not_started", gotTask.Progress, "closing a backlog must not move its tasks' progress")
}

func TestClosingAnEpicLeavesItsTasksAndBacklogAlone(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	task := f.createTask(t, "Still open", &f.backlog.ID)
	_, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{
		EpicID: e.ID, TaskIds: []uuid.UUID{task.ID},
	})
	require.NoError(t, err)

	_, err = f.q.CloseEpicForOwner(ctx, db.CloseEpicForOwnerParams{ID: e.ID, OwnerUserID: f.owner.ID})
	require.NoError(t, err)

	gotTask := f.taskByID(t, task.ID)
	assert.Equal(t, "open", gotTask.Status)
	assert.False(t, gotTask.ClosedAt.Valid)

	gotBacklog, err := f.q.GetBacklogForOwner(ctx, db.GetBacklogForOwnerParams{
		ID: f.backlog.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "open", gotBacklog.Status, "closing an epic must not close the backlog above it")
}

func TestReopenClearsClosedAt(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	_, err := f.q.CloseBacklogForOwner(ctx, db.CloseBacklogForOwnerParams{
		ID: f.backlog.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)

	reopened, err := f.q.ReopenBacklogForOwner(ctx, db.ReopenBacklogForOwnerParams{
		ID: f.backlog.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "open", reopened.Status)
	assert.False(t, reopened.ClosedAt.Valid)
}

// Both collections' open-only default is the feature, so the WHERE clause it
// rests on is worth pinning against real SQL and not only the fake's
// hand-written equivalent.
func TestListBacklogsAndEpicsFilterByStatus(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	closedBacklog, err := f.q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID: f.project.ID, Name: "Release 2.3", Priority: "medium", Progress: "not_started",
	})
	require.NoError(t, err)
	_, err = f.q.CloseBacklogForOwner(ctx, db.CloseBacklogForOwnerParams{
		ID: closedBacklog.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)

	openEpic := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	closedEpic := f.createEpic(t, "Endpoints", &f.backlog.ID, nil)
	_, err = f.q.CloseEpicForOwner(ctx, db.CloseEpicForOwnerParams{ID: closedEpic.ID, OwnerUserID: f.owner.ID})
	require.NoError(t, err)

	backlogNames := func(status string) []string {
		rows, err := f.q.ListBacklogsByProject(ctx, db.ListBacklogsByProjectParams{
			ProjectID: f.project.ID, Status: status,
		})
		require.NoError(t, err)
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = r.Name
		}
		return names
	}
	assert.ElementsMatch(t, []string{f.backlog.Name}, backlogNames("open"))
	assert.ElementsMatch(t, []string{closedBacklog.Name}, backlogNames("closed"))
	assert.ElementsMatch(t, []string{f.backlog.Name, closedBacklog.Name}, backlogNames(""),
		`an empty status is "no filter" at the SQL level; the open-only default lives in internal/backlog`)

	epicNames := func(status string) []string {
		rows, err := f.q.ListEpicsByProject(ctx, db.ListEpicsByProjectParams{
			ProjectID: f.project.ID, Status: status,
		})
		require.NoError(t, err)
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = r.Name
		}
		return names
	}
	assert.ElementsMatch(t, []string{openEpic.Name}, epicNames("open"))
	assert.ElementsMatch(t, []string{closedEpic.Name}, epicNames("closed"))
	assert.ElementsMatch(t, []string{openEpic.Name, closedEpic.Name}, epicNames(""))
}
