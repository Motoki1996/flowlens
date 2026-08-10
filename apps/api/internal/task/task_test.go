package task_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *task.Service {
	projects := project.NewService(q)
	backlogs := backlog.NewService(q, projects)
	return task.NewService(q, dbtest.FakeTxRunner{Q: q}, projects, backlogs)
}

// seedLinkedGitlabProject links a fake GitLab project (id 100) to
// connectionID directly at the database layer, bypassing
// linkedproject.Service — task tests only need a link to exist and be
// findable as a project's default, not the full linking flow. The first
// link created for a connection becomes its default (mirroring the real
// SQL), which is exactly what internal/task.Service.Create looks up.
func seedLinkedGitlabProject(t *testing.T, q *dbtest.FakeQuerier, connectionID uuid.UUID) db.LinkedGitlabProject {
	t.Helper()
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: connectionID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	return link
}

func TestService_Create_ValidatesTitle(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  Fix bug  ", "Fix bug", nil},
		{"rejects empty after trim", "   ", "", task.ErrInvalidTitle},
		{"rejects too long", strings.Repeat("a", 256), "", task.ErrInvalidTitle},
		{"accepts max length", strings.Repeat("a", 255), strings.Repeat("a", 255), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			got, err := svc.Create(context.Background(), owner, p.ID, task.CreateParams{Title: tt.input})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Title)
			assert.Equal(t, task.StatusOpen, got.Status)
			assert.Nil(t, got.BacklogID)
			assert.Nil(t, got.Gitlab)
		})
	}
}

func TestService_Create_DefaultsAndValidatesPriority(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"empty defaults to medium", "", task.PriorityMedium, nil},
		{"accepts low", task.PriorityLow, task.PriorityLow, nil},
		{"accepts high", task.PriorityHigh, task.PriorityHigh, nil},
		{"accepts urgent", task.PriorityUrgent, task.PriorityUrgent, nil},
		{"rejects unknown value", "critical", "", task.ErrInvalidPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			got, err := svc.Create(context.Background(), owner, p.ID, task.CreateParams{Title: "Fix bug", Priority: tt.input})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Priority)
		})
	}
}

func TestService_Update_ChangesPriority(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug", Priority: task.PriorityLow})
	require.NoError(t, err)
	require.Equal(t, task.PriorityLow, created.Priority)

	// An absent Priority leaves the stored value alone, the same as every
	// other Optional field on UpdateParams.
	untouched, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{Title: task.Present("Fix bug")})
	require.NoError(t, err)
	assert.Equal(t, task.PriorityLow, untouched.Priority)

	updated, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{
		Title:    task.Present("Fix bug"),
		Priority: task.Present(task.PriorityUrgent),
	})
	require.NoError(t, err)
	assert.Equal(t, task.PriorityUrgent, updated.Priority)

	_, err = svc.Update(ctx, owner, created.ID, task.UpdateParams{
		Title:    task.Present("Fix bug"),
		Priority: task.Present("not-a-priority"),
	})
	assert.ErrorIs(t, err, task.ErrInvalidPriority)
}

// Progress follows the same absent/explicit-empty rules as priority, and is
// deliberately independent of Status: closing a task must not move it.
func TestService_Update_ChangesProgress(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug"})
	require.NoError(t, err)
	require.Equal(t, task.ProgressNotStarted, created.Progress, "an absent progress defaults to not_started")

	untouched, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{Title: task.Present("Fix bug")})
	require.NoError(t, err)
	assert.Equal(t, task.ProgressNotStarted, untouched.Progress)

	updated, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{
		Progress: task.Present(task.ProgressOnHold),
	})
	require.NoError(t, err)
	assert.Equal(t, task.ProgressOnHold, updated.Progress)

	// Closing the task is the GitLab issue state changing; progress is the
	// app's own and must survive it untouched.
	closed, err := svc.Close(ctx, owner, created.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusClosed, closed.Status)
	assert.Equal(t, task.ProgressOnHold, closed.Progress)

	_, err = svc.Update(ctx, owner, created.ID, task.UpdateParams{
		Progress: task.Present("nearly-done"),
	})
	assert.ErrorIs(t, err, task.ErrInvalidProgress)
}

func TestService_List_FiltersAndSortsByProgress(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	// Created in the reverse of the progress order, so the sort proves it
	// overrides the manual position order rather than coinciding with it.
	done, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Done", Progress: task.ProgressDone})
	require.NoError(t, err)
	onHold, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "On hold", Progress: task.ProgressOnHold})
	require.NoError(t, err)
	inProgress, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "In progress", Progress: task.ProgressInProgress})
	require.NoError(t, err)
	notStarted, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Not started"})
	require.NoError(t, err)

	filtered, err := svc.List(ctx, owner, p.ID, task.ListFilter{Progress: task.ProgressOnHold})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, onHold.ID, filtered[0].ID)

	// Progress sorts the other way from priority: not_started first, done last.
	sorted, err := svc.List(ctx, owner, p.ID, task.ListFilter{Sort: task.SortProgress})
	require.NoError(t, err)
	require.Len(t, sorted, 4)
	assert.Equal(t, []uuid.UUID{notStarted.ID, inProgress.ID, onHold.ID, done.ID}, []uuid.UUID{
		sorted[0].ID, sorted[1].ID, sorted[2].ID, sorted[3].ID,
	})
}

func TestService_Create_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Create(context.Background(), other, p.ID, task.CreateParams{Title: "Fix bug"})
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_Create_RejectsBacklogFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(owner, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")

	_, err := svc.Create(context.Background(), owner, p.ID, task.CreateParams{
		Title:     "Fix bug",
		BacklogID: &foreignBacklog.ID,
	})
	assert.ErrorIs(t, err, task.ErrBacklogNotInProject)
}

func TestService_AssignBacklog_RejectsBacklogFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(owner, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.AssignBacklog(context.Background(), owner, tsk.ID, &foreignBacklog.ID)
	assert.ErrorIs(t, err, task.ErrBacklogNotInProject)

	still, err := svc.Get(context.Background(), owner, tsk.ID)
	require.NoError(t, err)
	assert.Nil(t, still.BacklogID)
}

func TestService_AssignBacklog_AssignsWithinSameProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	got, err := svc.AssignBacklog(context.Background(), owner, tsk.ID, &b.ID)
	require.NoError(t, err)
	require.NotNil(t, got.BacklogID)
	assert.Equal(t, b.ID, *got.BacklogID)
}

func TestService_AssignBacklog_NilReturnsTaskToUnfiled(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	tsk := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Fix bug")

	got, err := svc.AssignBacklog(context.Background(), owner, tsk.ID, nil)
	require.NoError(t, err)
	assert.Nil(t, got.BacklogID)
}

func TestService_List_FiltersByBacklogAndStatus(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b1 := q.SeedBacklog(p.ID, "Sprint 1")
	b2 := q.SeedBacklog(p.ID, "Sprint 2")

	inB1 := q.SeedTaskInBacklog(p.ID, b1.ID, owner, "In sprint 1")
	inB2 := q.SeedTaskInBacklog(p.ID, b2.ID, owner, "In sprint 2")
	unfiled := q.SeedTask(p.ID, owner, "Unfiled")

	closedUnfiled, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Closed unfiled"})
	require.NoError(t, err)
	_, err = svc.Close(ctx, owner, closedUnfiled.ID)
	require.NoError(t, err)

	tests := []struct {
		name   string
		filter task.ListFilter
		want   []uuid.UUID
	}{
		{"no filter returns everything", task.ListFilter{}, []uuid.UUID{inB1.ID, inB2.ID, unfiled.ID, closedUnfiled.ID}},
		{"backlog_id=unassigned returns only unfiled tasks", task.ListFilter{Unassigned: true}, []uuid.UUID{unfiled.ID, closedUnfiled.ID}},
		{"backlog_id=<id> scopes to one backlog", task.ListFilter{BacklogID: &b1.ID}, []uuid.UUID{inB1.ID}},
		{"status=open excludes closed tasks", task.ListFilter{Status: task.StatusOpen}, []uuid.UUID{inB1.ID, inB2.ID, unfiled.ID}},
		{"unassigned+status=closed combine", task.ListFilter{Unassigned: true, Status: task.StatusClosed}, []uuid.UUID{closedUnfiled.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.List(ctx, owner, p.ID, tt.filter)
			require.NoError(t, err)
			ids := make([]uuid.UUID, len(got))
			for i, tk := range got {
				ids[i] = tk.ID
			}
			assert.ElementsMatch(t, tt.want, ids)
		})
	}
}

func TestService_List_FiltersByQuery(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	titleHit, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix login bug", Description: "unrelated"})
	require.NoError(t, err)
	descriptionHit, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Unrelated", Description: "investigate login timeout"})
	require.NoError(t, err)
	urgentHit, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Login page redesign", Priority: task.PriorityUrgent})
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Something else entirely"})
	require.NoError(t, err)
	japanese, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "ログイン画面の修正"})
	require.NoError(t, err)

	tests := []struct {
		name   string
		filter task.ListFilter
		want   []uuid.UUID
	}{
		{"matches title", task.ListFilter{Query: "login"}, []uuid.UUID{titleHit.ID, descriptionHit.ID, urgentHit.ID}},
		{"matches description", task.ListFilter{Query: "timeout"}, []uuid.UUID{descriptionHit.ID}},
		{"combines with another filter", task.ListFilter{Query: "login", Priority: task.PriorityUrgent}, []uuid.UUID{urgentHit.ID}},
		{"matches a Japanese title", task.ListFilter{Query: "ログイン"}, []uuid.UUID{japanese.ID}},
		{"no match returns nothing", task.ListFilter{Query: "no-such-word"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.List(ctx, owner, p.ID, tt.filter)
			require.NoError(t, err)
			ids := make([]uuid.UUID, len(got))
			for i, tk := range got {
				ids[i] = tk.ID
			}
			assert.ElementsMatch(t, tt.want, ids)
		})
	}
}

func TestService_List_FiltersAndSortsByPriority(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	low, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Low", Priority: task.PriorityLow})
	require.NoError(t, err)
	medium, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Medium", Priority: task.PriorityMedium})
	require.NoError(t, err)
	high, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "High", Priority: task.PriorityHigh})
	require.NoError(t, err)
	urgent, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Urgent", Priority: task.PriorityUrgent})
	require.NoError(t, err)

	filtered, err := svc.List(ctx, owner, p.ID, task.ListFilter{Priority: task.PriorityHigh})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, high.ID, filtered[0].ID)

	// Sorting by priority ranks urgent > high > medium > low; the fixture
	// was created in the opposite (low-to-urgent, i.e. position ASC) order,
	// so this also proves the sort overrides the manual position order.
	sorted, err := svc.List(ctx, owner, p.ID, task.ListFilter{Sort: task.SortPriority})
	require.NoError(t, err)
	require.Len(t, sorted, 4)
	assert.Equal(t, []uuid.UUID{urgent.ID, high.ID, medium.ID, low.ID}, []uuid.UUID{
		sorted[0].ID, sorted[1].ID, sorted[2].ID, sorted[3].ID,
	})

	// Without Sort, the original position order (creation order) is
	// unaffected by priority.
	unsorted, err := svc.List(ctx, owner, p.ID, task.ListFilter{})
	require.NoError(t, err)
	require.Len(t, unsorted, 4)
	assert.Equal(t, []uuid.UUID{low.ID, medium.ID, high.ID, urgent.ID}, []uuid.UUID{
		unsorted[0].ID, unsorted[1].ID, unsorted[2].ID, unsorted[3].ID,
	})
}

// The project-scoped list accepts the same sort values as the cross-project
// one; dueOn and updatedAt are ordered in the service rather than in SQL, so
// they get their own cases here.
func TestService_List_SortsByDueDateAndRecency(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	early := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	undated, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Undated"})
	require.NoError(t, err)
	dueLate, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Late", DueOn: &late})
	require.NoError(t, err)
	dueEarly, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Early", DueOn: &early})
	require.NoError(t, err)

	byDueOn, err := svc.List(ctx, owner, p.ID, task.ListFilter{Sort: task.SortDueOn})
	require.NoError(t, err)
	require.Len(t, byDueOn, 3)
	assert.Equal(t, []uuid.UUID{dueEarly.ID, dueLate.ID, undated.ID}, []uuid.UUID{
		byDueOn[0].ID, byDueOn[1].ID, byDueOn[2].ID,
	}, "a task with no due date sorts last")

	// Touching a task in the middle of the manual order makes it the most
	// recently updated one, so this can't pass on position order by accident.
	touched, err := svc.Update(ctx, owner, dueLate.ID, task.UpdateParams{Title: task.Present("Late, edited")})
	require.NoError(t, err)

	byUpdatedAt, err := svc.List(ctx, owner, p.ID, task.ListFilter{Sort: task.SortUpdatedAt})
	require.NoError(t, err)
	require.Len(t, byUpdatedAt, 3)
	assert.Equal(t, touched.ID, byUpdatedAt[0].ID, "the most recently updated task comes first")
}

func TestService_List_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.List(context.Background(), other, p.ID, task.ListFilter{})
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_ListForOwner_SpansEveryOwnedProjectAndCarriesProjectName(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	alpha := q.SeedProject(owner, "Alpha")
	beta := q.SeedProject(owner, "Beta")

	inAlpha, err := svc.Create(ctx, owner, alpha.ID, task.CreateParams{Title: "In alpha"})
	require.NoError(t, err)
	inBeta, err := svc.Create(ctx, owner, beta.ID, task.CreateParams{Title: "In beta"})
	require.NoError(t, err)

	got, err := svc.ListForOwner(ctx, owner, task.CrossProjectFilter{})
	require.NoError(t, err)
	require.Len(t, got, 2)

	byID := map[uuid.UUID]task.TaskWithProject{}
	for _, tk := range got {
		byID[tk.ID] = tk
	}
	assert.Equal(t, "Alpha", byID[inAlpha.ID].ProjectName)
	assert.Equal(t, "Beta", byID[inBeta.ID].ProjectName)
}

// TestService_ListForOwner_NeverLeaksAnotherOwnersTasks is the completion
// condition issue #76 calls out explicitly: a cross-project query is exactly
// the shape of bug that could leak another user's tasks if the owner filter
// were ever dropped, so this is asserted directly rather than left to the
// per-project tests' coverage.
func TestService_ListForOwner_NeverLeaksAnotherOwnersTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	mine := q.SeedProject(owner, "Mine")
	theirs := q.SeedProject(other, "Theirs")

	myTask, err := svc.Create(ctx, owner, mine.ID, task.CreateParams{Title: "Mine"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, other, theirs.ID, task.CreateParams{Title: "Theirs"})
	require.NoError(t, err)

	got, err := svc.ListForOwner(ctx, owner, task.CrossProjectFilter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, myTask.ID, got[0].ID)

	// Even asking for the other owner's project by ID directly must not
	// surface it — ProjectIDs only ever narrows within the caller's own
	// projects, never a way to reach someone else's.
	got, err = svc.ListForOwner(ctx, owner, task.CrossProjectFilter{ProjectIDs: []uuid.UUID{theirs.ID}})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestService_ListForOwner_FiltersByStatusPriorityAndDates(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	soon := time.Now().AddDate(0, 0, 1)
	later := time.Now().AddDate(0, 0, 10)

	dueSoonUrgent, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
		Title: "Due soon, urgent", Priority: task.PriorityUrgent, DueOn: &soon,
	})
	require.NoError(t, err)
	dueLater, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
		Title: "Due later", Priority: task.PriorityLow, DueOn: &later,
	})
	require.NoError(t, err)
	closed, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Closed", DueOn: &soon})
	require.NoError(t, err)
	_, err = svc.Close(ctx, owner, closed.ID)
	require.NoError(t, err)

	tests := []struct {
		name   string
		filter task.CrossProjectFilter
		want   []uuid.UUID
	}{
		{"status=open excludes closed", task.CrossProjectFilter{Status: task.StatusOpen}, []uuid.UUID{dueSoonUrgent.ID, dueLater.ID}},
		{"priority narrows to one task", task.CrossProjectFilter{Priority: task.PriorityUrgent}, []uuid.UUID{dueSoonUrgent.ID}},
		{"dueBefore excludes the later task", task.CrossProjectFilter{DueBefore: timePtr(soon.AddDate(0, 0, 1))}, []uuid.UUID{dueSoonUrgent.ID, closed.ID}},
		{"dueAfter excludes the soon tasks", task.CrossProjectFilter{DueAfter: timePtr(soon.AddDate(0, 0, 1))}, []uuid.UUID{dueLater.ID}},
		{"query narrows by title", task.CrossProjectFilter{Query: "urgent"}, []uuid.UUID{dueSoonUrgent.ID}},
		{"query combines with priority", task.CrossProjectFilter{Query: "due", Priority: task.PriorityLow}, []uuid.UUID{dueLater.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.ListForOwner(ctx, owner, tt.filter)
			require.NoError(t, err)
			ids := make([]uuid.UUID, len(got))
			for i, tk := range got {
				ids[i] = tk.ID
			}
			assert.ElementsMatch(t, tt.want, ids)
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func TestService_ListForOwner_Sorts(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	soon := time.Now().AddDate(0, 0, 1)
	later := time.Now().AddDate(0, 0, 10)

	noDueDate, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "No due date", Priority: task.PriorityLow})
	require.NoError(t, err)
	dueLater, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Due later", Priority: task.PriorityUrgent, DueOn: &later})
	require.NoError(t, err)
	dueSoon, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Due soon", Priority: task.PriorityLow, DueOn: &soon})
	require.NoError(t, err)

	// Default (no Sort given) behaves like sort=dueOn: ascending due date,
	// with the no-due-date task sinking to the bottom.
	byDueOn, err := svc.ListForOwner(ctx, owner, task.CrossProjectFilter{})
	require.NoError(t, err)
	require.Len(t, byDueOn, 3)
	assert.Equal(t, []uuid.UUID{dueSoon.ID, dueLater.ID, noDueDate.ID}, []uuid.UUID{
		byDueOn[0].ID, byDueOn[1].ID, byDueOn[2].ID,
	})

	// sort=priority ranks urgent first regardless of due date.
	byPriority, err := svc.ListForOwner(ctx, owner, task.CrossProjectFilter{Sort: task.SortPriority})
	require.NoError(t, err)
	require.Len(t, byPriority, 3)
	assert.Equal(t, dueLater.ID, byPriority[0].ID)
}

func TestService_ListForOwner_CapsAtLimit(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	for i := 0; i < 3; i++ {
		_, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: fmt.Sprintf("Task %d", i)})
		require.NoError(t, err)
	}

	got, err := svc.ListForOwner(ctx, owner, task.CrossProjectFilter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestService_Close_IsIdempotent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	first, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusClosed, first.Status)
	require.NotNil(t, first.ClosedAt)

	second, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, second.ClosedAt)
	assert.Equal(t, first.ClosedAt.UnixNano(), second.ClosedAt.UnixNano())
}

func TestService_Reopen_IsIdempotent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	// Reopening an already-open task is a no-op.
	opened, err := svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, opened.Status)
	assert.Nil(t, opened.ClosedAt)

	_, err = svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)

	first, err := svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, first.Status)
	assert.Nil(t, first.ClosedAt)

	second, err := svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, second.Status)
	assert.Nil(t, second.ClosedAt)
}

func TestService_Delete_RemovesTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	require.NoError(t, svc.Delete(context.Background(), owner, tsk.ID))

	_, err := svc.Get(context.Background(), owner, tsk.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForMissingTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	assert.ErrorIs(t, svc.Delete(context.Background(), owner, uuid.New()), task.ErrNotFound)
}

func TestService_UpsertAIContext_CreatesThenUpdates(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	created, err := svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Given/When/Then",
		AIContext:          "Legacy payments module",
		AllowedScope:       "internal/payments/**",
		ForbiddenScope:     "internal/auth/**",
	})
	require.NoError(t, err)
	assert.Equal(t, "Given/When/Then", created.AcceptanceCriteria)
	assert.Equal(t, "Legacy payments module", created.AIContext)
	assert.Equal(t, "internal/payments/**", created.AllowedScope)
	assert.Equal(t, "internal/auth/**", created.ForbiddenScope)
	require.NotNil(t, created.UpdatedAt)

	got, err := svc.GetAIContext(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	updated, err := svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Updated criteria",
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated criteria", updated.AcceptanceCriteria)
	// Fields omitted from the second call overwrite, they don't merge.
	assert.Equal(t, "", updated.AIContext)
	assert.Equal(t, "", updated.AllowedScope)
	assert.Equal(t, "", updated.ForbiddenScope)

	got, err = svc.GetAIContext(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, updated, got)
}

func TestService_GetAIContext_ReturnsZeroValueWhenNeverSet(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	got, err := svc.GetAIContext(context.Background(), owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.AIContext{}, got)
}

func TestService_UpsertAIContext_RejectsFieldOverLengthLimit(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.UpsertAIContext(context.Background(), owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: strings.Repeat("a", 20001),
	})
	assert.ErrorIs(t, err, task.ErrAIContextFieldTooLong)
}

func TestService_UpsertAIContext_ForeignTaskGetsNotFound(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.UpsertAIContext(context.Background(), other, tsk.ID, task.AIContextParams{AcceptanceCriteria: "x"})
	assert.ErrorIs(t, err, task.ErrNotFound)

	got, err := svc.GetAIContext(context.Background(), owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.AIContext{}, got, "a rejected write from a non-owner must not land")
}

// This is the invariant the sync feature depends on: an AI-context-only edit
// must never look like a task edit, or it would spuriously enqueue a sync
// job once the outbox worker ships (docs/plans/issue-sync.md).
func TestService_UpsertAIContext_DoesNotChangeTaskUpdatedAt(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	before, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)

	_, err = svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Given/When/Then",
		AIContext:          "Legacy payments module",
		AllowedScope:       "internal/payments/**",
		ForbiddenScope:     "internal/auth/**",
	})
	require.NoError(t, err)

	after, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, before.UpdatedAt.UnixNano(), after.UpdatedAt.UnixNano())
}

// BuildGitlabIssuePayload must never reference task_ai_contexts fields, since
// they are app-only and must never be sent to GitLab (docs/plans/issue-sync.md,
// "Why the task is split across three tables"). This is pinned as a
// prerequisite for the GitLab issue sync feature.
func TestBuildGitlabIssuePayload_NeverReferencesAIContextFields(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	tk, err := svc.Update(ctx, owner, tsk.ID, task.UpdateParams{
		Title:       task.Present("Fix bug"),
		Description: task.Present("does not mention acceptance criteria"),
		Labels:      task.Present([]string{"bug"}),
	})
	require.NoError(t, err)

	_, err = svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "SECRET_ACCEPTANCE_CRITERIA",
		AIContext:          "SECRET_AI_CONTEXT",
		AllowedScope:       "SECRET_ALLOWED_SCOPE",
		ForbiddenScope:     "SECRET_FORBIDDEN_SCOPE",
	})
	require.NoError(t, err)

	payload := task.BuildGitlabIssuePayload(tk)

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	for _, secret := range []string{
		"SECRET_ACCEPTANCE_CRITERIA", "SECRET_AI_CONTEXT", "SECRET_ALLOWED_SCOPE", "SECRET_FORBIDDEN_SCOPE",
	} {
		assert.NotContains(t, string(body), secret)
	}
	assert.Equal(t, "Fix bug", payload.Title)
	assert.Equal(t, "does not mention acceptance criteria", payload.Description)
	assert.Equal(t, []string{"bug"}, payload.Labels)
}

// Ownership is enforced through the parent project, so a non-owner is told
// the task does not exist for reads and is refused for writes.
func TestService_ScopesEveryOperationToProjectOwner(t *testing.T) {
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		tsk := q.SeedTask(p.ID, owner, "Fix bug")

		_, err := svc.Get(ctx, other, tsk.ID)
		assert.ErrorIs(t, err, task.ErrNotFound)
	})

	t.Run("update leaves the task untouched", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		tsk := q.SeedTask(p.ID, owner, "Fix bug")

		_, err := svc.Update(ctx, other, tsk.ID, task.UpdateParams{Title: task.Present("Hijacked")})
		require.ErrorIs(t, err, task.ErrNotFound)

		still, err := svc.Get(ctx, owner, tsk.ID)
		require.NoError(t, err)
		assert.Equal(t, "Fix bug", still.Title)
	})

	t.Run("delete leaves the task in place", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		tsk := q.SeedTask(p.ID, owner, "Fix bug")

		err := svc.Delete(ctx, other, tsk.ID)
		require.ErrorIs(t, err, task.ErrNotFound)

		_, err = svc.Get(ctx, owner, tsk.ID)
		assert.NoError(t, err)
	})
}

// The following tests cover the outbound sync wiring
// (docs/plans/issue-sync.md, "Outbound"): Create/Update/Close/Reopen
// enqueue the right sync_jobs row, in the right cases, with the right
// payload — and Delete never does.

func TestService_Create_EnqueuesIssueCreateJob_WhenProjectHasDefaultLinkedGitlabProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)

	got, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
		Title:       "Fix bug",
		Description: "does the thing",
		Labels:      []string{"bug"},
	})
	require.NoError(t, err)

	jobs := q.SyncJobsForTask(got.ID)
	require.Len(t, jobs, 1)
	assert.Equal(t, issuesync.KindIssueCreate, jobs[0].Kind)

	var payload issuesync.CreatePayload
	require.NoError(t, json.Unmarshal(jobs[0].Payload, &payload))
	assert.Equal(t, link.ID, payload.LinkedGitlabProjectID)
	assert.Equal(t, "Fix bug", payload.Title)
	assert.Equal(t, "does the thing", payload.Description)
	assert.Equal(t, []string{"bug"}, payload.Labels)
}

func TestService_Create_DoesNotEnqueue_WhenProjectHasNoLinkedGitlabProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	got, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug"})
	require.NoError(t, err)
	assert.Empty(t, q.SyncJobsForTask(got.ID))
}

func TestService_Create_DefaultsAssigneeToConnectionTokenIdentity(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           p.ID,
		BaseUrl:             "https://gitlab.example.com",
		EncryptedToken:      []byte("encrypted"),
		TokenGitlabUserID:   pgtype.Int8{Int64: 42, Valid: true},
		TokenGitlabUsername: "octocat-bot",
	})
	require.NoError(t, err)
	seedLinkedGitlabProject(t, q, conn.ID)

	got, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug"})
	require.NoError(t, err)
	require.NotNil(t, got.AssigneeGitlabUserID)
	assert.EqualValues(t, 42, *got.AssigneeGitlabUserID)
	assert.Equal(t, "octocat-bot", got.AssigneeGitlabUsername)
}

func TestService_Create_ExplicitAssigneeOverridesConnectionDefault(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           p.ID,
		BaseUrl:             "https://gitlab.example.com",
		EncryptedToken:      []byte("encrypted"),
		TokenGitlabUserID:   pgtype.Int8{Int64: 42, Valid: true},
		TokenGitlabUsername: "octocat-bot",
	})
	require.NoError(t, err)
	seedLinkedGitlabProject(t, q, conn.ID)

	explicit := int64(7)
	got, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
		Title:                  "Fix bug",
		AssigneeGitlabUserID:   &explicit,
		AssigneeGitlabUsername: "someone-else",
	})
	require.NoError(t, err)
	require.NotNil(t, got.AssigneeGitlabUserID)
	assert.EqualValues(t, 7, *got.AssigneeGitlabUserID)
	assert.Equal(t, "someone-else", got.AssigneeGitlabUsername)
}

func TestService_Update_EnqueuesIssueUpdateJob_WhenMirroredFieldsChangeAndTaskIsLinked(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.Update(ctx, owner, tsk.ID, task.UpdateParams{
		Title:       task.Present("Fix bug harder"),
		Description: task.Present("more detail"),
		Labels:      task.Present([]string{"bug", "urgent"}),
	})
	require.NoError(t, err)

	jobs := q.SyncJobsForTask(tsk.ID)
	require.Len(t, jobs, 1)
	assert.Equal(t, issuesync.KindIssueUpdate, jobs[0].Kind)
	assert.Equal(t, fmt.Sprintf("issue.update:%s", tsk.ID), jobs[0].DedupeKey.String)

	var payload issuesync.UpdatePayload
	require.NoError(t, json.Unmarshal(jobs[0].Payload, &payload))
	require.NotNil(t, payload.Title)
	assert.Equal(t, "Fix bug harder", *payload.Title)
	assert.Equal(t, []string{"bug", "urgent"}, payload.Labels)
}

func TestService_Update_DoesNotEnqueue_WhenTaskHasNoGitlabLink(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.Update(ctx, owner, tsk.ID, task.UpdateParams{Title: task.Present("Fix bug harder"), Description: task.Present("x")})
	require.NoError(t, err)
	assert.Empty(t, q.SyncJobsForTask(tsk.ID))
}

func TestService_Update_DoesNotEnqueue_WhenOnlyBacklogOrPositionChange(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.Update(ctx, owner, tsk.ID, task.UpdateParams{
		Title:    task.Present("Fix bug"), // unchanged
		Position: task.Present(int32(5)),  // app-only, never mirrored to GitLab
	})
	require.NoError(t, err)
	assert.Empty(t, q.SyncJobsForTask(tsk.ID))
}

// Update is a partial update: a field left unset in UpdateParams keeps its
// current value, and a nullable field explicitly set to nil is cleared. This
// is what lets the web edit form PATCH one attribute without echoing the
// whole task back — and what keeps position from being reset to 0 by a form
// that never shows it.
func TestService_Update_PartialUpdate(t *testing.T) {
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	assigneeID := int64(42)

	seed := func(t *testing.T) (*task.Service, context.Context, uuid.UUID, task.Task) {
		t.Helper()
		q := dbtest.New()
		svc := newService(q)
		ctx := context.Background()
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
			Title:                  "Fix bug",
			Description:            "original",
			AssigneeGitlabUserID:   &assigneeID,
			AssigneeGitlabUsername: "octocat",
			Labels:                 []string{"bug"},
			DueOn:                  &due,
			StartDate:              &start,
		})
		require.NoError(t, err)
		return svc, ctx, owner, created
	}

	tests := []struct {
		name   string
		params task.UpdateParams
		want   func(t *testing.T, before, after task.Task)
	}{
		{
			name:   "title only leaves every other field alone",
			params: task.UpdateParams{Title: task.Present("Renamed")},
			want: func(t *testing.T, before, after task.Task) {
				assert.Equal(t, "Renamed", after.Title)
				assert.Equal(t, before.Description, after.Description)
				assert.Equal(t, before.Labels, after.Labels)
				assert.Equal(t, before.AssigneeGitlabUsername, after.AssigneeGitlabUsername)
				assert.Equal(t, before.AssigneeGitlabUserID, after.AssigneeGitlabUserID)
				assert.Equal(t, before.Position, after.Position)
				require.NotNil(t, after.DueOn)
				assert.True(t, due.Equal(*after.DueOn))
				require.NotNil(t, after.StartDate)
				assert.True(t, start.Equal(*after.StartDate))
			},
		},
		{
			name:   "explicit null clears a nullable field",
			params: task.UpdateParams{DueOn: task.Present[*time.Time](nil)},
			want: func(t *testing.T, before, after task.Task) {
				assert.Nil(t, after.DueOn)
				assert.Equal(t, before.Title, after.Title)
				require.NotNil(t, after.StartDate, "an untouched nullable field is not cleared")
			},
		},
		{
			name:   "empty params change nothing",
			params: task.UpdateParams{},
			want: func(t *testing.T, before, after task.Task) {
				assert.Equal(t, before.Title, after.Title)
				assert.Equal(t, before.Description, after.Description)
				assert.Equal(t, before.Labels, after.Labels)
				assert.Equal(t, before.Position, after.Position)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, ctx, owner, before := seed(t)
			after, err := svc.Update(ctx, owner, before.ID, tt.params)
			require.NoError(t, err)
			tt.want(t, before, after)
		})
	}
}

// startDate is app-only (issue #33): GitLab Issues have no native start-date
// field, so, unlike due_on, it must round-trip through Create/Update without
// ever being pushed to GitLab.
func TestService_Create_PersistsStartDate(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug", StartDate: &start})
	require.NoError(t, err)
	require.NotNil(t, created.StartDate)
	assert.True(t, start.Equal(*created.StartDate))

	got, err := svc.Get(ctx, owner, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.StartDate)
	assert.True(t, start.Equal(*got.StartDate))
}

// startDate is never mirrored to GitLab (it has no due_on-style
// counterpart), so an update that only changes it must not enqueue an
// issue.update job, even for an already-linked task — mirroring
// TestService_Update_DoesNotEnqueue_WhenOnlyBacklogOrPositionChange.
func TestService_Update_PersistsStartDate_WithoutEnqueuingSyncJob(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	updated, err := svc.Update(ctx, owner, tsk.ID, task.UpdateParams{
		Title:     task.Present("Fix bug"), // unchanged
		StartDate: task.Present(&start),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.StartDate)
	assert.True(t, start.Equal(*updated.StartDate))
	assert.Empty(t, q.SyncJobsForTask(tsk.ID))
}

// This is the invariant the sync feature depends on (see the same-named
// assertion in TestBuildGitlabIssuePayload_NeverReferencesAIContextFields):
// an AI-context-only edit must never look like a task edit, so it must
// never enqueue a sync job either, even for an already-linked task.
func TestService_UpsertAIContext_DoesNotEnqueueSyncJob(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{AcceptanceCriteria: "x"})
	require.NoError(t, err)
	assert.Empty(t, q.SyncJobsForTask(tsk.ID))
}

func TestService_Close_EnqueuesIssueCloseJob_WhenTaskIsLinked(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)

	jobs := q.SyncJobsForTask(tsk.ID)
	require.Len(t, jobs, 1)
	assert.Equal(t, issuesync.KindIssueClose, jobs[0].Kind)
}

func TestService_Close_DoesNotEnqueue_WhenTaskHasNoGitlabLink(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Empty(t, q.SyncJobsForTask(tsk.ID))
}

func TestService_Close_DoesNotEnqueue_WhenAlreadyClosed(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.Len(t, q.SyncJobsForTask(tsk.ID), 1)

	_, err = svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Len(t, q.SyncJobsForTask(tsk.ID), 1, "closing an already-closed task must not enqueue a second job")
}

func TestService_Reopen_EnqueuesIssueReopenJob_WhenTaskIsLinked(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)

	_, err = svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)

	jobs := q.SyncJobsForTask(tsk.ID)
	require.Len(t, jobs, 2)
	assert.Equal(t, issuesync.KindIssueClose, jobs[0].Kind)
	assert.Equal(t, issuesync.KindIssueReopen, jobs[1].Kind)
}

func TestService_Reopen_DoesNotEnqueue_WhenTaskHasNoGitlabLink(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	_, err = svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Empty(t, q.SyncJobsForTask(tsk.ID))
}

// Delete must never touch GitLab, so it must never enqueue a sync job
// either — even for an already-linked task — per docs/plans/issue-sync.md's
// "Task deletion" rule.
func TestService_Delete_NeverEnqueuesSyncJob(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	require.NoError(t, svc.Delete(ctx, owner, tsk.ID))
	assert.Zero(t, q.SyncJobCount())
}

func TestService_Get_ReportsGitlabNil_WhenTaskNeverLinked(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	got, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Gitlab, "a task whose project never had a linked GitLab project is purely local")
}

func TestService_Get_ReportsSyncedFromLink(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	got, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Gitlab)
	assert.Equal(t, task.SyncStatusSynced, got.Gitlab.SyncStatus)
	require.NotNil(t, got.Gitlab.IssueIID)
	assert.Equal(t, int64(7), *got.Gitlab.IssueIID)
}

func TestService_Get_ReportsPending_WhenIssueCreateJobStillPending(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	q.SeedSyncJobForTask(tsk.ID, p.ID, issuesync.KindIssueCreate, "pending", "")

	got, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Gitlab)
	assert.Equal(t, task.SyncStatusPending, got.Gitlab.SyncStatus)
}

func TestService_Get_ReportsFailed_WhenIssueCreateJobExhausted(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	q.SeedSyncJobForTask(tsk.ID, p.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	got, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Gitlab)
	assert.Equal(t, task.SyncStatusFailed, got.Gitlab.SyncStatus)
	assert.Equal(t, "gitlab unreachable", got.Gitlab.LastError)
}

func TestService_Get_ReportsFailedFromLink_WhenPushFailed(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)
	_, err := q.MarkTaskGitlabLinkFailedForTask(ctx, db.MarkTaskGitlabLinkFailedForTaskParams{
		TaskID: tsk.ID, LastError: "gitlab rejected the update",
	})
	require.NoError(t, err)

	got, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Gitlab)
	assert.Equal(t, task.SyncStatusFailed, got.Gitlab.SyncStatus)
	assert.Equal(t, "gitlab rejected the update", got.Gitlab.LastError)
}

func TestService_RetrySync_ReturnsConflict_WhenNotFailed(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	_, err := svc.RetrySync(ctx, owner, tsk.ID)
	assert.ErrorIs(t, err, task.ErrSyncNotFailed)
}

func TestService_RetrySync_ReturnsConflict_WhenNeverLinkedOrEnqueued(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.RetrySync(ctx, owner, tsk.ID)
	assert.ErrorIs(t, err, task.ErrSyncNotFailed)
}

func TestService_RetrySync_ReturnsNotFoundForForeignTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.RetrySync(ctx, other, tsk.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_RetrySync_ResetsAlreadyLinkedTaskToPending(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link := seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)
	_, err := q.MarkTaskGitlabLinkFailedForTask(ctx, db.MarkTaskGitlabLinkFailedForTaskParams{
		TaskID: tsk.ID, LastError: "gitlab rejected the update",
	})
	require.NoError(t, err)
	job := q.SeedSyncJobForTask(tsk.ID, p.ID, issuesync.KindIssueUpdate, "failed", "gitlab rejected the update")

	got, err := svc.RetrySync(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Gitlab)
	assert.Equal(t, task.SyncStatusPending, got.Gitlab.SyncStatus, "the UI must reflect the retry immediately, not wait for the worker")
	assert.Empty(t, got.Gitlab.LastError)

	reset, ok := q.GetSyncJob(job.ID)
	require.True(t, ok)
	assert.Equal(t, "pending", reset.Status)
	assert.Zero(t, reset.Attempts, "retry gives the job a fresh attempt budget")
}

func TestService_RetrySync_ResetsNeverLinkedTaskToPending(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	q.SeedSyncJobForTask(tsk.ID, p.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	got, err := svc.RetrySync(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Gitlab)
	assert.Equal(t, task.SyncStatusPending, got.Gitlab.SyncStatus)
}

func TestService_Reorder_AppliesGivenOrderWithinBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	first := q.SeedTaskInBacklog(p.ID, b.ID, owner, "First")
	second := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Second")
	third := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Third")

	got, err := svc.Reorder(ctx, owner, p.ID, &b.ID, []uuid.UUID{third.ID, first.ID, second.ID})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []uuid.UUID{third.ID, first.ID, second.ID}, []uuid.UUID{got[0].ID, got[1].ID, got[2].ID})
	assert.Equal(t, int32(0), got[0].Position)
	assert.Equal(t, int32(1), got[1].Position)
	assert.Equal(t, int32(2), got[2].Position)
}

func TestService_Reorder_AppliesGivenOrderWithinUnclassified(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	first := q.SeedTask(p.ID, owner, "First")
	second := q.SeedTask(p.ID, owner, "Second")

	got, err := svc.Reorder(ctx, owner, p.ID, nil, []uuid.UUID{second.ID, first.ID})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, second.ID, got[0].ID)
	assert.Equal(t, first.ID, got[1].ID)
}

func TestService_Reorder_RejectsMismatchedTaskIDs(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	first := q.SeedTaskInBacklog(p.ID, b.ID, owner, "First")
	second := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Second")

	tests := []struct {
		name    string
		taskIDs []uuid.UUID
	}{
		{"missing a task", []uuid.UUID{first.ID}},
		{"duplicates a task instead of including every one", []uuid.UUID{first.ID, first.ID}},
		{"includes a foreign task", []uuid.UUID{first.ID, second.ID, uuid.New()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Reorder(ctx, owner, p.ID, &b.ID, tt.taskIDs)
			assert.ErrorIs(t, err, task.ErrTaskIDsMismatch)
		})
	}

	// Nothing was written by the rejected calls above.
	unchanged, err := svc.Get(ctx, owner, first.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(0), unchanged.Position)
}

func TestService_Reorder_RejectsBacklogFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(owner, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")

	_, err := svc.Reorder(ctx, owner, p.ID, &foreignBacklog.ID, nil)
	assert.ErrorIs(t, err, task.ErrBacklogNotInProject)
}

func TestService_Reorder_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Reorder(context.Background(), other, p.ID, nil, nil)
	assert.ErrorIs(t, err, task.ErrNotFound)
}
