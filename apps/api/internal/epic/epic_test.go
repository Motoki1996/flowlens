package epic_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/epic"
	"github.com/flowlens/api/internal/optional"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *epic.Service {
	return epic.NewService(q, dbtest.FakeTxRunner{Q: q}, project.NewService(q))
}

// seedLink links a GitLab project to projectID's connection, creating the
// connection too. gitlabProjectID keeps two links in the same test distinct.
func seedLink(t *testing.T, q *dbtest.FakeQuerier, projectID uuid.UUID, gitlabProjectID int64) db.LinkedGitlabProject {
	t.Helper()
	conn := q.SeedGitlabConnection(projectID, []byte("encrypted"))
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    gitlabProjectID,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	return link
}

// The shared field rules (name, priority, progress, base branch, scope) live
// in internal/fieldnorm and are exercised in depth through internal/backlog.
// What matters here is that internal/epic wraps them and reports its *own*
// sentinels, which is what the HTTP layer's error mapping keys on.
func TestService_Create_ValidatesSharedFields(t *testing.T) {
	tests := []struct {
		name    string
		params  epic.CreateParams
		wantErr error
	}{
		{"trimmed empty name", epic.CreateParams{Name: "   "}, epic.ErrInvalidName},
		{"name too long", epic.CreateParams{Name: strings.Repeat("a", 101)}, epic.ErrInvalidName},
		{"unknown priority", epic.CreateParams{Name: "Screens", Priority: "critical"}, epic.ErrInvalidPriority},
		{"unknown progress", epic.CreateParams{Name: "Screens", Progress: "started"}, epic.ErrInvalidProgress},
		{"invalid base branch", epic.CreateParams{Name: "Screens", BaseBranch: "feature branch"}, epic.ErrInvalidBaseBranch},
		{"scope too long", epic.CreateParams{Name: "Screens", AllowedScope: strings.Repeat("a", 20001)}, epic.ErrInvalidScope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			_, err := svc.Create(context.Background(), owner, p.ID, tt.params)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestService_Create_DefaultsAndTrims(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	e, err := svc.Create(context.Background(), owner, p.ID, epic.CreateParams{Name: "  Screens  ", BaseBranch: "  develop  "})
	require.NoError(t, err)
	assert.Equal(t, "Screens", e.Name)
	assert.Equal(t, "develop", e.BaseBranch)
	assert.Equal(t, epic.PriorityMedium, e.Priority)
	assert.Equal(t, epic.ProgressNotStarted, e.Progress)
	assert.Nil(t, e.BacklogID)
}

// An epic filed in a backlog is the whole point of the layer; one filed in
// *another project's* backlog would appear in that project's screens while
// carrying this project's tasks, and no FK can catch it.
func TestService_Create_ValidatesBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	other := q.SeedProject(owner, "Beta")
	own := q.SeedBacklog(p.ID, "Sprint 1")
	foreign := q.SeedBacklog(other.ID, "Their sprint")

	e, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &own.ID})
	require.NoError(t, err)
	require.NotNil(t, e.BacklogID)
	assert.Equal(t, own.ID, *e.BacklogID)

	_, err = svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &foreign.ID})
	assert.ErrorIs(t, err, epic.ErrBacklogNotInProject)

	missing := uuid.New()
	_, err = svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &missing})
	assert.ErrorIs(t, err, epic.ErrBacklogNotInProject)
}

func TestService_Create_ValidatesDefaultLinkedGitlabProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	other := q.SeedProject(owner, "Beta")
	own := seedLink(t, q, p.ID, 100)
	foreign := seedLink(t, q, other.ID, 200)

	e, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", DefaultLinkedGitlabProjectID: &own.ID})
	require.NoError(t, err)
	require.NotNil(t, e.DefaultLinkedGitlabProjectID)
	assert.Equal(t, own.ID, *e.DefaultLinkedGitlabProjectID)

	_, err = svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", DefaultLinkedGitlabProjectID: &foreign.ID})
	assert.ErrorIs(t, err, epic.ErrLinkNotInProject)
}

func TestService_Create_ValidatesAssigneeIsMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	stranger := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	e, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", AssigneeUserID: &owner})
	require.NoError(t, err)
	require.NotNil(t, e.AssigneeUserID)
	assert.Equal(t, "octocat", e.AssigneeUsername)

	_, err = svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", AssigneeUserID: &stranger})
	assert.ErrorIs(t, err, epic.ErrAssigneeNotMember)
}

// An epic in another user's project is indistinguishable from a missing one,
// the posture every project-scoped service in this codebase takes.
func TestService_ForeignProjectIsNotFound(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	stranger := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens"})
	require.NoError(t, err)

	_, err = svc.Get(ctx, stranger, created.ID)
	assert.ErrorIs(t, err, epic.ErrNotFound)

	_, err = svc.Update(ctx, stranger, created.ID, epic.UpdateParams{Name: "Renamed"})
	assert.ErrorIs(t, err, epic.ErrNotFound)

	assert.ErrorIs(t, svc.Delete(ctx, stranger, created.ID), epic.ErrNotFound)

	_, err = svc.List(ctx, stranger, p.ID, epic.ListFilter{})
	assert.ErrorIs(t, err, epic.ErrNotFound)
}

// An absent field on a PATCH keeps its stored value: the UPDATE writes every
// column, so anything the caller left out has to be read back first.
func TestService_Update_KeepsAbsentFields(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{
		Name:           "Screens",
		BacklogID:      &b.ID,
		BaseBranch:     "develop",
		AllowedScope:   "apps/web/**",
		ForbiddenScope: "migrations/**",
		Priority:       epic.PriorityHigh,
	})
	require.NoError(t, err)

	renamed, err := svc.Update(ctx, owner, created.ID, epic.UpdateParams{Name: "Screens v2"})
	require.NoError(t, err)
	assert.Equal(t, "Screens v2", renamed.Name)
	assert.Equal(t, "develop", renamed.BaseBranch)
	assert.Equal(t, "apps/web/**", renamed.AllowedScope)
	assert.Equal(t, "migrations/**", renamed.ForbiddenScope)
	assert.Equal(t, epic.PriorityHigh, renamed.Priority)
	require.NotNil(t, renamed.BacklogID)
	assert.Equal(t, b.ID, *renamed.BacklogID)
}

// An explicit empty string clears an optional text field; an explicit null
// clears a nullable one.
func TestService_Update_ExplicitlyClearsFields(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{
		Name: "Screens", BacklogID: &b.ID, BaseBranch: "develop", AssigneeUserID: &owner,
	})
	require.NoError(t, err)

	cleared, err := svc.Update(ctx, owner, created.ID, epic.UpdateParams{
		Name:           "Screens",
		BaseBranch:     optional.Present(""),
		BacklogID:      optional.Present[*uuid.UUID](nil),
		AssigneeUserID: optional.Present[*uuid.UUID](nil),
	})
	require.NoError(t, err)
	assert.Equal(t, "", cleared.BaseBranch)
	assert.Nil(t, cleared.BacklogID)
	assert.Nil(t, cleared.AssigneeUserID)
	assert.Equal(t, "", cleared.AssigneeUsername)
}

// A task's backlog and its epic's must agree, so moving an epic between
// backlogs takes its tasks with it — otherwise the task would report a
// backlog its own epic no longer belongs to.
func TestService_Update_MovesTasksWithTheEpic(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	from := q.SeedBacklog(p.ID, "Sprint 1")
	to := q.SeedBacklog(p.ID, "Sprint 2")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &from.ID})
	require.NoError(t, err)

	task := q.SeedTaskInBacklog(p.ID, from.ID, owner, "Build the list screen")
	q.SeedTaskEpic(task.ID, created.ID)

	_, err = svc.Update(ctx, owner, created.ID, epic.UpdateParams{Name: "Screens", BacklogID: optional.Present(&to.ID)})
	require.NoError(t, err)

	stored := q.TaskByID(task.ID)
	require.True(t, stored.BacklogID.Valid)
	assert.Equal(t, to.ID, uuid.UUID(stored.BacklogID.Bytes))
}

func TestService_Update_RejectsForeignBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	other := q.SeedProject(owner, "Beta")
	foreign := q.SeedBacklog(other.ID, "Their sprint")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens"})
	require.NoError(t, err)

	_, err = svc.Update(ctx, owner, created.ID, epic.UpdateParams{Name: "Screens", BacklogID: optional.Present(&foreign.ID)})
	assert.ErrorIs(t, err, epic.ErrBacklogNotInProject)
}

func TestService_List_FiltersByBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	filed, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &b.ID})
	require.NoError(t, err)
	unfiled, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Loose ends"})
	require.NoError(t, err)

	inBacklog, err := svc.List(ctx, owner, p.ID, epic.ListFilter{BacklogID: &b.ID})
	require.NoError(t, err)
	require.Len(t, inBacklog, 1)
	assert.Equal(t, filed.ID, inBacklog[0].ID)

	none, err := svc.List(ctx, owner, p.ID, epic.ListFilter{BacklogUnfiled: true})
	require.NoError(t, err)
	require.Len(t, none, 1)
	assert.Equal(t, unfiled.ID, none[0].ID)

	all, err := svc.List(ctx, owner, p.ID, epic.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

// The task counts come from the list query's aggregate, so the collection
// screen never fetches a project's tasks just to draw a ratio.
func TestService_List_CountsTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens"})
	require.NoError(t, err)

	open := q.SeedTask(p.ID, owner, "Open one")
	closed := q.SeedTask(p.ID, owner, "Closed one")
	q.SeedTaskEpic(open.ID, created.ID)
	q.SeedTaskEpic(closed.ID, created.ID)
	q.SeedTaskStatus(closed.ID, "closed")

	list, err := svc.List(ctx, owner, p.ID, epic.ListFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(2), list[0].TaskCount)
	assert.Equal(t, int64(1), list[0].ClosedTaskCount)
}

// Deleting an epic drops its tasks back to sitting directly in their backlog —
// exactly where they were before the epic existed — rather than deleting them.
func TestService_Delete_UnfilesTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &b.ID})
	require.NoError(t, err)
	task := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Build the list screen")
	q.SeedTaskEpic(task.ID, created.ID)

	require.NoError(t, svc.Delete(ctx, owner, created.ID))

	stored := q.TaskByID(task.ID)
	assert.False(t, stored.EpicID.Valid)
	require.True(t, stored.BacklogID.Valid)
	assert.Equal(t, b.ID, uuid.UUID(stored.BacklogID.Bytes))

	_, err = svc.Get(ctx, owner, created.ID)
	assert.ErrorIs(t, err, epic.ErrNotFound)
}

// SetTasks is the epic's own half of the task↔epic relationship — the half a
// picker over a backlog's tasks writes. Declarative: the caller sends the
// whole set.
func TestService_SetTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &b.ID})
	require.NoError(t, err)

	first := q.SeedTask(p.ID, owner, "Build the list screen")
	second := q.SeedTask(p.ID, owner, "Build the detail screen")

	require.NoError(t, svc.SetTasks(ctx, owner, created.ID, []uuid.UUID{first.ID, second.ID}))

	// Filed under the epic — and moved to its backlog, since the two must
	// agree even though these tasks were unfiled a moment ago.
	for _, id := range []uuid.UUID{first.ID, second.ID} {
		stored := q.TaskByID(id)
		require.True(t, stored.EpicID.Valid)
		assert.Equal(t, created.ID, uuid.UUID(stored.EpicID.Bytes))
		require.True(t, stored.BacklogID.Valid)
		assert.Equal(t, b.ID, uuid.UUID(stored.BacklogID.Bytes))
	}

	// Dropping one from the set unfiles it from the epic but leaves it in the
	// backlog — the same thing deleting the epic would do.
	require.NoError(t, svc.SetTasks(ctx, owner, created.ID, []uuid.UUID{first.ID}))
	dropped := q.TaskByID(second.ID)
	assert.False(t, dropped.EpicID.Valid)
	require.True(t, dropped.BacklogID.Valid)
	assert.Equal(t, b.ID, uuid.UUID(dropped.BacklogID.Bytes))

	// An empty set empties the epic.
	require.NoError(t, svc.SetTasks(ctx, owner, created.ID, nil))
	assert.False(t, q.TaskByID(first.ID).EpicID.Valid)
}

func TestService_SetTasks_RejectsForeignTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	other := q.SeedProject(owner, "Beta")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens"})
	require.NoError(t, err)

	own := q.SeedTask(p.ID, owner, "Ours")
	foreign := q.SeedTask(other.ID, owner, "Theirs")

	// All-or-nothing: the valid id in the same request must not move either.
	err = svc.SetTasks(ctx, owner, created.ID, []uuid.UUID{own.ID, foreign.ID})
	assert.ErrorIs(t, err, epic.ErrTaskNotInProject)
	assert.False(t, q.TaskByID(own.ID).EpicID.Valid)
	assert.False(t, q.TaskByID(foreign.ID).EpicID.Valid)

	missing := uuid.New()
	assert.ErrorIs(t, svc.SetTasks(ctx, owner, created.ID, []uuid.UUID{missing}), epic.ErrTaskNotInProject)
}

func TestService_SetTasks_ForeignEpicIsNotFound(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	stranger := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens"})
	require.NoError(t, err)
	tsk := q.SeedTask(p.ID, owner, "Ours")

	assert.ErrorIs(t, svc.SetTasks(ctx, stranger, created.ID, []uuid.UUID{tsk.ID}), epic.ErrNotFound)
	assert.False(t, q.TaskByID(tsk.ID).EpicID.Valid)
}

// estimatedPoints is the pre-breakdown estimate (000033). It is nullable on
// purpose — "nobody has estimated this" is a distinct answer from any number,
// which is why 0 is rejected rather than accepted as a shorthand for it.
func TestService_EstimatedPoints(t *testing.T) {
	zero, negative, five := 0, -3, 5

	tests := []struct {
		name string
		// create runs first; update, when non-nil, runs on the created epic.
		create     epic.CreateParams
		update     *epic.UpdateParams
		wantErr    error
		wantPoints *int
	}{
		{
			name:       "created unestimated",
			create:     epic.CreateParams{Name: "Screens"},
			wantPoints: nil,
		},
		{
			name:       "created with an estimate",
			create:     epic.CreateParams{Name: "Screens", EstimatedPoints: &five},
			wantPoints: &five,
		},
		{
			name:    "zero is rejected on create",
			create:  epic.CreateParams{Name: "Screens", EstimatedPoints: &zero},
			wantErr: epic.ErrInvalidEstimate,
		},
		{
			name:    "negative is rejected on create",
			create:  epic.CreateParams{Name: "Screens", EstimatedPoints: &negative},
			wantErr: epic.ErrInvalidEstimate,
		},
		{
			name:       "absent on update keeps the stored estimate",
			create:     epic.CreateParams{Name: "Screens", EstimatedPoints: &five},
			update:     &epic.UpdateParams{Name: "Screens"},
			wantPoints: &five,
		},
		{
			name:       "an explicit null clears it",
			create:     epic.CreateParams{Name: "Screens", EstimatedPoints: &five},
			update:     &epic.UpdateParams{Name: "Screens", EstimatedPoints: optional.Present[*int](nil)},
			wantPoints: nil,
		},
		{
			name:       "an estimate can be added after the fact",
			create:     epic.CreateParams{Name: "Screens"},
			update:     &epic.UpdateParams{Name: "Screens", EstimatedPoints: optional.Present(&five)},
			wantPoints: &five,
		},
		{
			name:    "zero is rejected on update",
			create:  epic.CreateParams{Name: "Screens", EstimatedPoints: &five},
			update:  &epic.UpdateParams{Name: "Screens", EstimatedPoints: optional.Present(&zero)},
			wantErr: epic.ErrInvalidEstimate,
		},
		{
			name:    "negative is rejected on update",
			create:  epic.CreateParams{Name: "Screens"},
			update:  &epic.UpdateParams{Name: "Screens", EstimatedPoints: optional.Present(&negative)},
			wantErr: epic.ErrInvalidEstimate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			ctx := context.Background()
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			got, err := svc.Create(ctx, owner, p.ID, tt.create)
			if tt.update == nil {
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
					return
				}
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
				got, err = svc.Update(ctx, owner, got.ID, *tt.update)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
					return
				}
				require.NoError(t, err)
			}

			if tt.wantPoints == nil {
				assert.Nil(t, got.EstimatedPoints)
				return
			}
			require.NotNil(t, got.EstimatedPoints)
			assert.Equal(t, *tt.wantPoints, *got.EstimatedPoints)
		})
	}
}

// The estimate survives the epic being broken down. It stops being consulted
// (EffectivePoints prefers the tasks' sum) but is never deleted: the guess and
// the eventual real breakdown side by side are the only data an
// estimate-vs-actual calibration could be built from.
func TestService_EstimatedPoints_SurvivesBreakdown(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	eight := 8
	created, err := svc.Create(ctx, owner, p.ID, epic.CreateParams{Name: "Screens", BacklogID: &b.ID, EstimatedPoints: &eight})
	require.NoError(t, err)

	task := q.SeedTask(p.ID, owner, "Build the list view")
	require.NoError(t, svc.SetTasks(ctx, owner, created.ID, []uuid.UUID{task.ID}))

	got, err := svc.Get(ctx, owner, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.EstimatedPoints)
	assert.Equal(t, 8, *got.EstimatedPoints)
}

// EffectivePoints is the one place the two point sources are reconciled: the
// tasks win once they exist, the estimate stands in until then, and neither
// present is *unknown* rather than zero — collapsing that last case to 0 is
// the bug issue #234 was filed for.
func TestEffectivePoints(t *testing.T) {
	taskPoints, estimate := 12, 5

	tests := []struct {
		name       string
		taskPoints *int
		estimate   *int
		wantPoints int
		wantKnown  bool
	}{
		{"tasks win over the estimate", &taskPoints, &estimate, 12, true},
		{"tasks alone", &taskPoints, nil, 12, true},
		{"estimate stands in while there are no tasks", nil, &estimate, 5, true},
		{"neither is unknown, not zero", nil, nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points, known := epic.EffectivePoints(tt.taskPoints, tt.estimate)
			assert.Equal(t, tt.wantKnown, known)
			assert.Equal(t, tt.wantPoints, points)
		})
	}
}
