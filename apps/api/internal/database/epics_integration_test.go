//go:build integration

// The epic queries (000032/000033/000035) against a live PostgreSQL, because
// three of the guarantees internal/epic rests on are the database's and not
// the service's: ON DELETE SET NULL on tasks.epic_id, the estimated_points
// CHECK, and the NOT EXISTS anti-join velocity's forecast is built on. The
// in-memory fake (dbtest) hand-implements all three, so a unit test passing
// against it proves the fake agrees with itself, not that the SQL is right.
//
// Run with: make test-integration
package database_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// epicFixture is one project with a backlog, owned by a member, ready to hang
// epics and tasks off. Every test here needs the same four rows.
type epicFixture struct {
	q       *db.Queries
	owner   db.User
	project db.Project
	backlog db.Backlog
}

func newEpicFixture(t *testing.T, q *db.Queries) epicFixture {
	t.Helper()
	ctx := context.Background()

	owner := createUser(t, q, "epic-owner")
	p, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: owner.ID, Name: "Alpha"})
	require.NoError(t, err)
	// Every epic query authorizes through project_members, not projects.owner_user_id.
	_, err = q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: p.ID, UserID: owner.ID, Role: "owner"})
	require.NoError(t, err)
	b, err := q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID: p.ID, Name: "Sprint 1", Priority: "medium", Progress: "not_started",
	})
	require.NoError(t, err)

	return epicFixture{q: q, owner: owner, project: p, backlog: b}
}

func (f epicFixture) createEpic(t *testing.T, name string, backlogID *uuid.UUID, points *int32) db.Epic {
	t.Helper()
	arg := db.CreateEpicParams{
		ProjectID: f.project.ID,
		Name:      name,
		Priority:  "medium",
		Progress:  "not_started",
	}
	if backlogID != nil {
		arg.BacklogID = pgtype.UUID{Bytes: *backlogID, Valid: true}
	}
	if points != nil {
		arg.EstimatedPoints = pgtype.Int4{Int32: *points, Valid: true}
	}
	e, err := f.q.CreateEpic(context.Background(), arg)
	require.NoError(t, err)
	return e
}

func (f epicFixture) createTask(t *testing.T, title string, backlogID *uuid.UUID) db.Task {
	t.Helper()
	arg := db.CreateTaskParams{
		ProjectID:       f.project.ID,
		Title:           title,
		Labels:          []string{},
		Priority:        "medium",
		Progress:        "not_started",
		Size:            "m",
		CreatedByUserID: f.owner.ID,
	}
	if backlogID != nil {
		arg.BacklogID = pgtype.UUID{Bytes: *backlogID, Valid: true}
	}
	task, err := f.q.CreateTask(context.Background(), arg)
	require.NoError(t, err)
	return task
}

func (f epicFixture) taskByID(t *testing.T, id uuid.UUID) db.Task {
	t.Helper()
	task, err := f.q.GetTaskForOwner(context.Background(), db.GetTaskForOwnerParams{ID: id, OwnerUserID: f.owner.ID})
	require.NoError(t, err)
	return task
}

// tasks.epic_id is declared ON DELETE SET NULL (000032). The whole of
// epic.Service.Delete's contract — "deleting an epic unfiles its tasks, never
// deletes them, and they keep their backlog" — is that clause and nothing
// else: the service issues no UPDATE of its own.
func TestDeletingEpicUnfilesItsTasksWithoutDeletingThem(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	task := f.createTask(t, "Build the list screen", &f.backlog.ID)
	rows, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{EpicID: e.ID, TaskIds: []uuid.UUID{task.ID}})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	deleted, err := f.q.DeleteEpicForOwner(ctx, db.DeleteEpicForOwnerParams{ID: e.ID, OwnerUserID: f.owner.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	stored := f.taskByID(t, task.ID)
	assert.False(t, stored.EpicID.Valid, "the FK must null the epic, not cascade the delete")
	require.True(t, stored.BacklogID.Valid, "the task must stay in its backlog")
	assert.Equal(t, f.backlog.ID, uuid.UUID(stored.BacklogID.Bytes))
}

// The estimated_points CHECK (000033) is the database half of "0 is rejected
// so that unestimated stays distinguishable from any number". The service
// validates it too, but the constraint is what makes it true for any writer.
func TestEpicEstimatedPointsCheckConstraint(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	t.Run("null is accepted and means unestimated", func(t *testing.T) {
		e := f.createEpic(t, "Unestimated", &f.backlog.ID, nil)
		assert.False(t, e.EstimatedPoints.Valid)
	})

	t.Run("a positive value round-trips", func(t *testing.T) {
		points := int32(21)
		e := f.createEpic(t, "Estimated", &f.backlog.ID, &points)
		require.True(t, e.EstimatedPoints.Valid)
		assert.Equal(t, int32(21), e.EstimatedPoints.Int32)
	})

	for _, points := range []int32{0, -1} {
		t.Run("the database refuses it", func(t *testing.T) {
			_, err := f.q.CreateEpic(ctx, db.CreateEpicParams{
				ProjectID:       f.project.ID,
				Name:            "Rejected",
				Priority:        "medium",
				Progress:        "not_started",
				EstimatedPoints: pgtype.Int4{Int32: points, Valid: true},
			})
			require.Error(t, err, "estimated_points = %d must violate the CHECK", points)
			assert.Contains(t, err.Error(), "estimated_points")
		})
	}
}

// AssignTasksToEpic writes the epic's own backlog onto every task it files,
// and refuses a task from another project. Both live entirely in the SQL.
func TestAssignTasksToEpicWritesTheEpicsBacklog(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	otherBacklog, err := f.q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID: f.project.ID, Name: "Sprint 2", Priority: "medium", Progress: "not_started",
	})
	require.NoError(t, err)

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	task := f.createTask(t, "Build the list screen", &otherBacklog.ID)

	rows, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{EpicID: e.ID, TaskIds: []uuid.UUID{task.ID}})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	stored := f.taskByID(t, task.ID)
	assert.Equal(t, f.backlog.ID, uuid.UUID(stored.BacklogID.Bytes), "the epic's backlog wins over the task's own")
}

// The corollary of the rule above, and the reason README calls it out: an
// epic filed nowhere writes *its* backlog — NULL — onto everything it adopts.
// Pinned here against the real UPDATE rather than only against the fake,
// since it is the one case where the rule costs the caller something.
func TestAssignTasksToUnfiledEpicUnfilesTheTasks(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	unfiled := f.createEpic(t, "Unfiled", nil, nil)
	require.False(t, unfiled.BacklogID.Valid)
	task := f.createTask(t, "Build the list screen", &f.backlog.ID)

	rows, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{EpicID: unfiled.ID, TaskIds: []uuid.UUID{task.ID}})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	stored := f.taskByID(t, task.ID)
	require.True(t, stored.EpicID.Valid)
	assert.False(t, stored.BacklogID.Valid, "the task leaves Sprint 1 with its epic")
}

// The project_id check inside AssignTasksToEpic is what stops an epic from
// adopting another project's task. It reports a short row count rather than
// erroring, which is what internal/epic rolls its transaction back on.
func TestAssignTasksToEpicIgnoresAForeignTask(t *testing.T) {
	q := testDB(t)
	f := newEpicFixture(t, q)
	other := newEpicFixture(t, q)
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	own := f.createTask(t, "Ours", &f.backlog.ID)
	foreign := other.createTask(t, "Theirs", &other.backlog.ID)

	rows, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{
		EpicID: e.ID, TaskIds: []uuid.UUID{own.ID, foreign.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), rows, "only the task in the epic's own project may move")
	assert.False(t, other.taskByID(t, foreign.ID).EpicID.Valid)
}

// ClearEpicTasksExcept is the other half of SetTasks. The empty-array case is
// the one worth pinning: NOT (id = ANY('{}')) has to be true for every row,
// so an empty set empties the epic rather than being a no-op.
func TestClearEpicTasksExceptEmptySetEmptiesTheEpic(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	first := f.createTask(t, "One", &f.backlog.ID)
	second := f.createTask(t, "Two", &f.backlog.ID)
	_, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{
		EpicID: e.ID, TaskIds: []uuid.UUID{first.ID, second.ID},
	})
	require.NoError(t, err)

	epicID := pgtype.UUID{Bytes: e.ID, Valid: true}

	// Keeping one drops the other, and the dropped task keeps its backlog.
	require.NoError(t, f.q.ClearEpicTasksExcept(ctx, db.ClearEpicTasksExceptParams{
		EpicID: epicID, TaskIds: []uuid.UUID{first.ID},
	}))
	dropped := f.taskByID(t, second.ID)
	assert.False(t, dropped.EpicID.Valid)
	assert.Equal(t, f.backlog.ID, uuid.UUID(dropped.BacklogID.Bytes))
	assert.True(t, f.taskByID(t, first.ID).EpicID.Valid)

	require.NoError(t, f.q.ClearEpicTasksExcept(ctx, db.ClearEpicTasksExceptParams{
		EpicID: epicID, TaskIds: []uuid.UUID{},
	}))
	assert.False(t, f.taskByID(t, first.ID).EpicID.Valid, "an empty set must empty the epic")
}

// MoveEpicTasksToBacklog runs in the same transaction as the epic's own
// update, so a task never reports a backlog its epic has left.
func TestMoveEpicTasksToBacklog(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	destination, err := f.q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID: f.project.ID, Name: "Sprint 2", Priority: "medium", Progress: "not_started",
	})
	require.NoError(t, err)

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	task := f.createTask(t, "Build the list screen", &f.backlog.ID)
	_, err = f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{EpicID: e.ID, TaskIds: []uuid.UUID{task.ID}})
	require.NoError(t, err)

	require.NoError(t, f.q.MoveEpicTasksToBacklog(ctx, db.MoveEpicTasksToBacklogParams{
		BacklogID: pgtype.UUID{Bytes: destination.ID, Valid: true},
		EpicID:    pgtype.UUID{Bytes: e.ID, Valid: true},
	}))

	assert.Equal(t, destination.ID, uuid.UUID(f.taskByID(t, task.ID).BacklogID.Bytes))
}

// ListEpicsByProject's LEFT JOIN task counts feed the collection's row count,
// board ratio and timeline fill. Aggregate SQL is exactly what the fake has
// to reimplement, so it is worth one real assertion.
func TestListEpicsByProjectCountsTasks(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)
	empty := f.createEpic(t, "Nothing yet", &f.backlog.ID, nil)

	open := f.createTask(t, "Open one", &f.backlog.ID)
	closed := f.createTask(t, "Closed one", &f.backlog.ID)
	_, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{
		EpicID: e.ID, TaskIds: []uuid.UUID{open.ID, closed.ID},
	})
	require.NoError(t, err)
	_, err = f.q.CloseTaskForOwner(ctx, db.CloseTaskForOwnerParams{ID: closed.ID, OwnerUserID: f.owner.ID})
	require.NoError(t, err)

	rows, err := f.q.ListEpicsByProject(ctx, db.ListEpicsByProjectParams{ProjectID: f.project.ID})
	require.NoError(t, err)

	counts := map[uuid.UUID][2]int64{}
	for _, row := range rows {
		counts[row.ID] = [2]int64{row.TaskCount, row.ClosedTaskCount}
	}
	assert.Equal(t, [2]int64{2, 1}, counts[e.ID])
	assert.Equal(t, [2]int64{0, 0}, counts[empty.ID], "an epic with no tasks must count 0, not vanish from the list")
}

// The NOT EXISTS anti-join in velocity.sql is the whole no-double-counting
// argument for the points forecast: an epic that has tasks is already counted
// task by task, so its estimate must not be added on top. Nothing about that
// is visible in Go — it is one SQL clause — and migration 000035 exists to
// index it, so it gets a real-Postgres test.
func TestListUnbrokenDownEpicEstimatesExcludesEpicsWithTasks(t *testing.T) {
	f := newEpicFixture(t, testDB(t))
	ctx := context.Background()

	twentyOne, three := int32(21), int32(3)
	unbrokenDown := f.createEpic(t, "Not started", &f.backlog.ID, &twentyOne)
	unestimated := f.createEpic(t, "No estimate", &f.backlog.ID, nil)
	brokenDown := f.createEpic(t, "Has tasks", &f.backlog.ID, &three)

	task := f.createTask(t, "Its one task", &f.backlog.ID)
	_, err := f.q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{EpicID: brokenDown.ID, TaskIds: []uuid.UUID{task.ID}})
	require.NoError(t, err)

	rows, err := f.q.ListUnbrokenDownEpicEstimatesForVelocity(ctx, db.ListUnbrokenDownEpicEstimatesForVelocityParams{
		ProjectID: f.project.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)

	got := map[uuid.UUID]pgtype.Int4{}
	for _, row := range rows {
		got[row.ID] = row.EstimatedPoints
	}
	assert.Len(t, got, 2)
	assert.Equal(t, int32(21), got[unbrokenDown.ID].Int32)
	assert.False(t, got[unestimated.ID].Valid, "an unestimated epic is returned, not filtered — the count is part of the answer")
	assert.NotContains(t, got, brokenDown.ID, "an epic with tasks must not contribute its estimate on top of them")

	// And the moment its last task leaves, it is back in the forecast: "has
	// tasks" is the only signal that the estimate has stopped being the truth.
	require.NoError(t, f.q.ClearEpicTasksExcept(ctx, db.ClearEpicTasksExceptParams{
		EpicID: pgtype.UUID{Bytes: brokenDown.ID, Valid: true}, TaskIds: []uuid.UUID{},
	}))
	rows, err = f.q.ListUnbrokenDownEpicEstimatesForVelocity(ctx, db.ListUnbrokenDownEpicEstimatesForVelocityParams{
		ProjectID: f.project.ID, OwnerUserID: f.owner.ID,
	})
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

// A non-member must not reach a project's epics at all — the same
// project_members check every other epic query carries.
func TestEpicQueriesEnforceMembership(t *testing.T) {
	q := testDB(t)
	f := newEpicFixture(t, q)
	intruder := createUser(t, q, "epic-intruder")
	ctx := context.Background()

	e := f.createEpic(t, "Screens", &f.backlog.ID, nil)

	_, err := q.GetEpicForOwner(ctx, db.GetEpicForOwnerParams{ID: e.ID, OwnerUserID: intruder.ID})
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	deleted, err := q.DeleteEpicForOwner(ctx, db.DeleteEpicForOwnerParams{ID: e.ID, OwnerUserID: intruder.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted, "a non-member's delete must write nothing")

	rows, err := q.ListEpicsByProject(ctx, db.ListEpicsByProjectParams{ProjectID: f.project.ID})
	require.NoError(t, err)
	assert.Len(t, rows, 1, "the epic itself must survive the refused delete")
}
