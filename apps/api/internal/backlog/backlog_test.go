package backlog_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/optional"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *backlog.Service {
	return backlog.NewService(q, dbtest.FakeTxRunner{Q: q}, project.NewService(q))
}

func TestService_Create_ValidatesName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  Sprint 1  ", "Sprint 1", nil},
		{"rejects empty after trim", "   ", "", backlog.ErrInvalidName},
		{"rejects too long", strings.Repeat("a", 101), "", backlog.ErrInvalidName},
		{"accepts max length", strings.Repeat("a", 100), strings.Repeat("a", 100), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			b, err := svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{Name: tt.input})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, b.Name)
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
		{"empty defaults to medium", "", backlog.PriorityMedium, nil},
		{"accepts low", backlog.PriorityLow, backlog.PriorityLow, nil},
		{"accepts urgent", backlog.PriorityUrgent, backlog.PriorityUrgent, nil},
		{"rejects unknown value", "critical", "", backlog.ErrInvalidPriority},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			got, err := svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{Name: "Sprint 1", Priority: tt.input})
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
	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", Priority: backlog.PriorityLow})
	require.NoError(t, err)
	require.Equal(t, backlog.PriorityLow, created.Priority)

	// An absent Priority leaves the stored value alone, the same as the two
	// dates on UpdateParams.
	untouched, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Sprint 1"}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, backlog.PriorityLow, untouched.Priority)

	updated, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1",
		Priority: optional.Present(backlog.PriorityUrgent),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, backlog.PriorityUrgent, updated.Priority)

	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1",
		Priority: optional.Present("not-a-priority"),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrInvalidPriority)
}

// Progress is the backlog's own four-stage work state, independent of the
// closed/total task ratio the UI derives separately.
func TestService_Update_ChangesProgress(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	require.Equal(t, backlog.ProgressNotStarted, created.Progress, "an absent progress defaults to not_started")

	untouched, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Sprint 1"}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, backlog.ProgressNotStarted, untouched.Progress)

	updated, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1",
		Progress: optional.Present(backlog.ProgressInProgress),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, backlog.ProgressInProgress, updated.Progress)

	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1",
		Progress: optional.Present("nearly-done"),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrInvalidProgress)
}

// Mirrors internal/task's TestService_Update_ChangingProgress_RecordsOneProgressEvent
// (issue #173's backlog-level counterpart).
func TestService_Update_ChangingProgress_RecordsOneProgressEvent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	assert.Empty(t, mustListBacklogProgressEvents(t, q, created.ID), "creating a backlog writes no progress event")

	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1",
		Progress: optional.Present(backlog.ProgressInProgress),
	}, backlog.ActorKindUser)
	require.NoError(t, err)

	events := mustListBacklogProgressEvents(t, q, created.ID)
	require.Len(t, events, 1)
	assert.Equal(t, backlog.ProgressNotStarted, events[0].FromProgress)
	assert.Equal(t, backlog.ProgressInProgress, events[0].ToProgress)
}

// A PATCH that leaves progress untouched, or re-sends the same value, must
// not write a progress event.
func TestService_Update_UnchangedProgress_RecordsNoProgressEvent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", Progress: backlog.ProgressInProgress})
	require.NoError(t, err)

	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Sprint 1, edited"}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Empty(t, mustListBacklogProgressEvents(t, q, created.ID))

	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1, edited",
		Progress: optional.Present(backlog.ProgressInProgress),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Empty(t, mustListBacklogProgressEvents(t, q, created.ID))
}

// actor_kind is taken from whatever Update's caller passes, mirroring
// internal/task's TestService_Update_ChangingProgress_AttributesActorKind.
func TestService_Update_ChangingProgress_AttributesActorKind(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:     "Sprint 1",
		Progress: optional.Present(backlog.ProgressInProgress),
	}, backlog.ActorKindUser)
	require.NoError(t, err)

	other := q.SeedUser("hubot", "hubot@example.com").ID
	p2 := q.SeedProject(other, "Beta")
	agentBacklog, err := svc.Create(ctx, other, p2.ID, backlog.CreateParams{Name: "Agent backlog"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, other, agentBacklog.ID, backlog.UpdateParams{
		Name:     "Agent backlog",
		Progress: optional.Present(backlog.ProgressInProgress),
	}, backlog.ActorKindAgent)
	require.NoError(t, err)

	userEvents := mustListBacklogProgressEvents(t, q, created.ID)
	require.Len(t, userEvents, 1)
	assert.Equal(t, backlog.ActorKindUser, userEvents[0].ActorKind)
	require.True(t, userEvents[0].ActorUserID.Valid)
	assert.Equal(t, owner, uuid.UUID(userEvents[0].ActorUserID.Bytes))

	agentEvents := mustListBacklogProgressEvents(t, q, agentBacklog.ID)
	require.Len(t, agentEvents, 1)
	assert.Equal(t, backlog.ActorKindAgent, agentEvents[0].ActorKind)
	assert.False(t, agentEvents[0].ActorUserID.Valid, "an agent-attributed event has no actor user")
}

func mustListBacklogProgressEvents(t *testing.T, q *dbtest.FakeQuerier, backlogID uuid.UUID) []db.BacklogProgressEvent {
	t.Helper()
	events, err := q.ListBacklogProgressEventsByBacklog(context.Background(), backlogID)
	require.NoError(t, err)
	return events
}

func TestService_List_FiltersAndSortsByProgress(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	// Created in the reverse of the progress order, so the sort proves it
	// overrides the manual position order.
	done, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Done", Progress: backlog.ProgressDone})
	require.NoError(t, err)
	onHold, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "On hold", Progress: backlog.ProgressOnHold})
	require.NoError(t, err)
	inProgress, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "In progress", Progress: backlog.ProgressInProgress})
	require.NoError(t, err)
	notStarted, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Not started"})
	require.NoError(t, err)

	filtered, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{Progress: backlog.ProgressOnHold})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, onHold.ID, filtered[0].ID)

	sorted, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{Sort: backlog.SortProgress})
	require.NoError(t, err)
	require.Len(t, sorted, 4)
	assert.Equal(t, []uuid.UUID{notStarted.ID, inProgress.ID, onHold.ID, done.ID}, []uuid.UUID{
		sorted[0].ID, sorted[1].ID, sorted[2].ID, sorted[3].ID,
	})
}

func TestService_List_FiltersAndSortsByPriority(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	low, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Low", Priority: backlog.PriorityLow})
	require.NoError(t, err)
	medium, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Medium", Priority: backlog.PriorityMedium})
	require.NoError(t, err)
	urgent, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Urgent", Priority: backlog.PriorityUrgent})
	require.NoError(t, err)

	filtered, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{Priority: backlog.PriorityUrgent})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, urgent.ID, filtered[0].ID)

	sorted, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{Sort: backlog.SortPriority})
	require.NoError(t, err)
	require.Len(t, sorted, 3)
	assert.Equal(t, []uuid.UUID{urgent.ID, medium.ID, low.ID}, []uuid.UUID{sorted[0].ID, sorted[1].ID, sorted[2].ID})
}

func TestService_Create_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Create(context.Background(), other, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	assert.ErrorIs(t, err, backlog.ErrNotFound)
}

func TestService_Create_ReturnsNotFoundForMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Create(context.Background(), owner, uuid.New(), backlog.CreateParams{Name: "Sprint 1"})
	assert.ErrorIs(t, err, backlog.ErrNotFound)
}

func TestService_List_ScopesToProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(owner, "Beta")
	ctx := context.Background()

	_, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 2"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, otherProject.ID, backlog.CreateParams{Name: "Unrelated"})
	require.NoError(t, err)

	backlogs, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{})
	require.NoError(t, err)
	require.Len(t, backlogs, 2)
	assert.Equal(t, "Sprint 1", backlogs[0].Name)
	assert.Equal(t, "Sprint 2", backlogs[1].Name)
}

// List's task counts come from ListBacklogsByProject's LEFT JOIN aggregate
// (issue #144): a backlog's own row reports its total and closed task counts,
// an empty backlog reports zero, and an unfiled task (backlog_id NULL) is not
// counted against anyone.
func TestService_List_ReturnsTaskCounts(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	sprint, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	empty, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Empty"})
	require.NoError(t, err)

	q.SeedTaskInBacklog(p.ID, sprint.ID, owner, "Open task")
	closed := q.SeedTaskInBacklog(p.ID, sprint.ID, owner, "Closed task")
	_, err = q.CloseTaskForOwner(ctx, db.CloseTaskForOwnerParams{ID: closed.ID, OwnerUserID: owner})
	require.NoError(t, err)
	q.SeedTask(p.ID, owner, "Unfiled task")

	backlogs, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{})
	require.NoError(t, err)
	require.Len(t, backlogs, 2)
	assert.Equal(t, int64(2), backlogs[0].TaskCount)
	assert.Equal(t, int64(1), backlogs[0].ClosedTaskCount)
	assert.Equal(t, empty.ID, backlogs[1].ID)
	assert.Equal(t, int64(0), backlogs[1].TaskCount)
	assert.Equal(t, int64(0), backlogs[1].ClosedTaskCount)
}

func TestService_List_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.List(context.Background(), other, p.ID, backlog.ListFilter{})
	assert.ErrorIs(t, err, backlog.ErrNotFound)
}

// Ownership is enforced through the parent project, so a non-owner is told
// the backlog does not exist for reads and is refused for writes.
func TestService_ScopesEveryOperationToProjectOwner(t *testing.T) {
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		b := q.SeedBacklog(p.ID, "Sprint 1")

		_, err := svc.Get(ctx, other, b.ID)
		assert.ErrorIs(t, err, backlog.ErrNotFound)
	})

	t.Run("update leaves the backlog untouched", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		b := q.SeedBacklog(p.ID, "Sprint 1")

		_, err := svc.Update(ctx, other, b.ID, backlog.UpdateParams{Name: "Hijacked"}, backlog.ActorKindUser)
		require.ErrorIs(t, err, backlog.ErrNotFound)

		still, err := svc.Get(ctx, owner, b.ID)
		require.NoError(t, err)
		assert.Equal(t, "Sprint 1", still.Name)
	})

	t.Run("delete leaves the backlog in place", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		b := q.SeedBacklog(p.ID, "Sprint 1")

		err := svc.Delete(ctx, other, b.ID)
		require.ErrorIs(t, err, backlog.ErrNotFound)

		_, err = svc.Get(ctx, owner, b.ID)
		assert.NoError(t, err)
	})
}

func TestService_Get_ReturnsNotFoundForMissingBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Get(context.Background(), owner, uuid.New())
	assert.ErrorIs(t, err, backlog.ErrNotFound)
}

func TestService_Update_ChangesNameAndDescription(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")

	updated, err := svc.Update(context.Background(), owner, b.ID, backlog.UpdateParams{
		Name:        "Renamed",
		Description: "new description",
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, "new description", updated.Description)
}

// date builds a UTC midnight time, the granularity a DATE column stores.
func date(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestService_Create_StoresSchedule(t *testing.T) {
	tests := []struct {
		name      string
		startDate *time.Time
		dueOn     *time.Time
		wantErr   error
	}{
		{"both dates", date(2026, 8, 1), date(2026, 8, 31), nil},
		{"due date only", nil, date(2026, 8, 31), nil},
		{"start date only", date(2026, 8, 1), nil, nil},
		{"neither", nil, nil, nil},
		{"same day", date(2026, 8, 1), date(2026, 8, 1), nil},
		{"start after due", date(2026, 9, 1), date(2026, 8, 31), backlog.ErrInvalidSchedule},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			b, err := svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{
				Name:      "Sprint 1",
				StartDate: tt.startDate,
				DueOn:     tt.dueOn,
			})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.startDate, b.StartDate)
			assert.Equal(t, tt.dueOn, b.DueOn)
		})
	}
}

// The rename form in the web UI sends only name/description/position, so an
// absent date has to mean "leave it alone" — otherwise every rename would
// silently wipe the backlog's planned period.
func TestService_Update_LeavesAbsentDatesUntouched(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{
		Name:      "Sprint 1",
		StartDate: date(2026, 8, 1),
		DueOn:     date(2026, 8, 31),
	})
	require.NoError(t, err)

	updated, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Renamed"}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, date(2026, 8, 1), updated.StartDate)
	assert.Equal(t, date(2026, 8, 31), updated.DueOn)
}

func TestService_Update_SetsAndClearsSchedule(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")

	set, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{
		Name:      "Sprint 1",
		StartDate: optional.Present(date(2026, 8, 1)),
		DueOn:     optional.Present(date(2026, 8, 31)),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, date(2026, 8, 1), set.StartDate)

	cleared, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{
		Name:      "Sprint 1",
		StartDate: optional.Present[*time.Time](nil),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Nil(t, cleared.StartDate)
	// The due date was absent from this request, so it survives the clear.
	assert.Equal(t, date(2026, 8, 31), cleared.DueOn)
}

// Validation runs against the resolved period, not against the request alone:
// a new start date that jumps past the *stored* due date is still invalid.
func TestService_Update_RejectsStartAfterStoredDue(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{
		Name:  "Sprint 1",
		DueOn: date(2026, 8, 31),
	})
	require.NoError(t, err)

	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:      "Sprint 1",
		StartDate: optional.Present(date(2026, 9, 15)),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrInvalidSchedule)

	unchanged, err := svc.Get(ctx, owner, created.ID)
	require.NoError(t, err)
	assert.Nil(t, unchanged.StartDate)
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

// The backlog's own issue destination (issue #180): set at create, kept
// through an unrelated update, cleared by an explicit null, and never allowed
// to point outside the backlog's own project.

func TestService_Create_SetsDefaultLinkedGitlabProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	link := seedLink(t, q, p.ID, 100)

	got, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{
		Name:                         "Sprint 1",
		DefaultLinkedGitlabProjectID: &link.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, got.DefaultLinkedGitlabProjectID)
	assert.Equal(t, link.ID, *got.DefaultLinkedGitlabProjectID)
}

func TestService_Create_DefaultsToNoLinkedGitlabProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	got, err := svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	assert.Nil(t, got.DefaultLinkedGitlabProjectID, "an unset link means the project default is used")
}

func TestService_Create_RejectsLinkedGitlabProjectOutsideProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	other := q.SeedProject(owner, "Beta")
	foreign := seedLink(t, q, other.ID, 200)

	_, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{
		Name:                         "Sprint 1",
		DefaultLinkedGitlabProjectID: &foreign.ID,
	})
	assert.ErrorIs(t, err, backlog.ErrLinkNotInProject)
}

func TestService_Update_SetsKeepsAndClearsDefaultLinkedGitlabProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	link := seedLink(t, q, p.ID, 100)
	b := q.SeedBacklog(p.ID, "Sprint 1")

	set, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{
		Name:                         "Sprint 1",
		DefaultLinkedGitlabProjectID: optional.Present(&link.ID),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	require.NotNil(t, set.DefaultLinkedGitlabProjectID)
	assert.Equal(t, link.ID, *set.DefaultLinkedGitlabProjectID)

	// A rename must not silently reset where this backlog's issues go.
	renamed, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{Name: "Renamed"}, backlog.ActorKindUser)
	require.NoError(t, err)
	require.NotNil(t, renamed.DefaultLinkedGitlabProjectID)
	assert.Equal(t, link.ID, *renamed.DefaultLinkedGitlabProjectID)

	cleared, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{
		Name:                         "Renamed",
		DefaultLinkedGitlabProjectID: optional.Present[*uuid.UUID](nil),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Nil(t, cleared.DefaultLinkedGitlabProjectID)
}

func TestService_Update_RejectsLinkedGitlabProjectOutsideProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	other := q.SeedProject(owner, "Beta")
	foreign := seedLink(t, q, other.ID, 200)
	b := q.SeedBacklog(p.ID, "Sprint 1")

	_, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{
		Name:                         "Sprint 1",
		DefaultLinkedGitlabProjectID: optional.Present(&foreign.ID),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrLinkNotInProject)

	unchanged, err := svc.Get(ctx, owner, b.ID)
	require.NoError(t, err)
	assert.Nil(t, unchanged.DefaultLinkedGitlabProjectID)
}

func TestService_Delete_RemovesBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")

	require.NoError(t, svc.Delete(context.Background(), owner, b.ID))

	_, err := svc.Get(context.Background(), owner, b.ID)
	assert.ErrorIs(t, err, backlog.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForMissingBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	assert.ErrorIs(t, svc.Delete(context.Background(), owner, uuid.New()), backlog.ErrNotFound)
}

func TestService_Create_ValidatesBaseBranch(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"empty is not set", "", "", nil},
		{"trims whitespace", "  main  ", "main", nil},
		{"accepts a namespaced branch", "release/2.4", "release/2.4", nil},
		{"rejects too long", strings.Repeat("a", 256), "", backlog.ErrInvalidBaseBranch},
		{"rejects a space", "feature branch", "", backlog.ErrInvalidBaseBranch},
		{"rejects a leading slash", "/main", "", backlog.ErrInvalidBaseBranch},
		{"rejects a trailing slash", "main/", "", backlog.ErrInvalidBaseBranch},
		{"rejects a leading dot", ".main", "", backlog.ErrInvalidBaseBranch},
		{"rejects a double dot", "main..2", "", backlog.ErrInvalidBaseBranch},
		{"rejects a .lock suffix", "main.lock", "", backlog.ErrInvalidBaseBranch},
		{"rejects a tilde", "main~1", "", backlog.ErrInvalidBaseBranch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			got, err := svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{Name: "Sprint 1", BaseBranch: tt.input})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.BaseBranch)
		})
	}
}

func TestService_Update_SetsKeepsAndClearsBaseBranch(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", BaseBranch: "main"})
	require.NoError(t, err)

	// Absent leaves the stored value untouched.
	updated, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Sprint 1"}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "main", updated.BaseBranch)

	// Explicit value overwrites it.
	updated, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:       "Sprint 1",
		BaseBranch: optional.Present("develop"),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "develop", updated.BaseBranch)

	// Explicit empty string clears it back to "not set".
	updated, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:       "Sprint 1",
		BaseBranch: optional.Present(""),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "", updated.BaseBranch)

	// An invalid explicit value is rejected.
	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:       "Sprint 1",
		BaseBranch: optional.Present("bad branch"),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrInvalidBaseBranch)
}

func TestService_Create_ValidatesScope(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	got, err := svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{
		Name:           "Sprint 1",
		AllowedScope:   "internal/payments/**",
		ForbiddenScope: "internal/auth/**",
	})
	require.NoError(t, err)
	assert.Equal(t, "internal/payments/**", got.AllowedScope)
	assert.Equal(t, "internal/auth/**", got.ForbiddenScope)

	_, err = svc.Create(context.Background(), owner, p.ID, backlog.CreateParams{
		Name:         "Sprint 2",
		AllowedScope: strings.Repeat("a", 20001),
	})
	assert.ErrorIs(t, err, backlog.ErrInvalidScope)
}

func TestService_Update_SetsKeepsAndClearsScope(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{
		Name:           "Sprint 1",
		AllowedScope:   "internal/payments/**",
		ForbiddenScope: "internal/auth/**",
	})
	require.NoError(t, err)

	// Absent leaves the stored values untouched.
	updated, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Sprint 1"}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "internal/payments/**", updated.AllowedScope)
	assert.Equal(t, "internal/auth/**", updated.ForbiddenScope)

	// Explicit value overwrites it.
	updated, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:           "Sprint 1",
		AllowedScope:   optional.Present("cmd/**"),
		ForbiddenScope: optional.Present("vendor/**"),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "cmd/**", updated.AllowedScope)
	assert.Equal(t, "vendor/**", updated.ForbiddenScope)

	// Explicit empty string clears it back to "not set".
	updated, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:           "Sprint 1",
		AllowedScope:   optional.Present(""),
		ForbiddenScope: optional.Present(""),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Equal(t, "", updated.AllowedScope)
	assert.Equal(t, "", updated.ForbiddenScope)

	// An invalid explicit value is rejected.
	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:           "Sprint 1",
		ForbiddenScope: optional.Present(strings.Repeat("a", 20001)),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrInvalidScope)
}

// --- Close / Reopen (000036) -------------------------------------------------

func TestService_Close_SetsStatusAndClosedAt(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	seeded := q.SeedBacklog(p.ID, "Release 2.4")

	before, err := svc.Get(context.Background(), owner, seeded.ID)
	require.NoError(t, err)
	assert.Equal(t, backlog.StatusOpen, before.Status)
	assert.Nil(t, before.ClosedAt)

	closed, err := svc.Close(context.Background(), owner, seeded.ID)
	require.NoError(t, err)
	assert.Equal(t, backlog.StatusClosed, closed.Status)
	require.NotNil(t, closed.ClosedAt)

	reopened, err := svc.Reopen(context.Background(), owner, seeded.ID)
	require.NoError(t, err)
	assert.Equal(t, backlog.StatusOpen, reopened.Status)
	assert.Nil(t, reopened.ClosedAt)
}

func TestService_Close_IsIdempotentAndLeavesClosedAtAlone(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	seeded := q.SeedBacklog(p.ID, "Release 2.4")

	first, err := svc.Close(context.Background(), owner, seeded.ID)
	require.NoError(t, err)
	require.NotNil(t, first.ClosedAt)

	second, err := svc.Close(context.Background(), owner, seeded.ID)
	require.NoError(t, err)
	assert.Equal(t, backlog.StatusClosed, second.Status)
	assert.Equal(t, first.ClosedAt, second.ClosedAt, "re-closing must not move closed_at")
}

// The rule the whole design turns on: a backlog's close is a statement about
// the backlog, never about the work inside it.
func TestService_Close_DoesNotCascadeToTasks(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Release 2.4")
	open := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Still open")

	_, err := svc.Close(context.Background(), owner, b.ID)
	require.NoError(t, err)

	task, err := q.GetTaskForOwner(context.Background(), db.GetTaskForOwnerParams{ID: open.ID, OwnerUserID: owner})
	require.NoError(t, err)
	assert.Equal(t, "open", task.Status, "closing a backlog must not close its tasks")
	assert.False(t, task.ClosedAt.Valid, "closing a backlog must not stamp a task's closed_at — internal/velocity reads it as a completion")
}

func TestService_List_HidesClosedByDefault(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   []string
	}{
		{"absent means open only", "", []string{"Open one"}},
		{"explicit open", backlog.StatusOpen, []string{"Open one"}},
		{"closed only", backlog.StatusClosed, []string{"Closed one"}},
		{"all", backlog.StatusAll, []string{"Open one", "Closed one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")
			q.SeedBacklog(p.ID, "Open one")
			closed := q.SeedBacklog(p.ID, "Closed one")
			_, err := svc.Close(context.Background(), owner, closed.ID)
			require.NoError(t, err)

			got, err := svc.List(context.Background(), owner, p.ID, backlog.ListFilter{Status: tt.status})
			require.NoError(t, err)
			names := make([]string, len(got))
			for i, b := range got {
				names[i] = b.Name
			}
			assert.ElementsMatch(t, tt.want, names)
		})
	}
}

// A closed backlog only leaves the *collection*; it stays reachable by ID, so
// a bookmark or a task's backlogId never dead-ends.
func TestService_Get_StillReturnsClosedBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	seeded := q.SeedBacklog(p.ID, "Release 2.4")
	_, err := svc.Close(context.Background(), owner, seeded.ID)
	require.NoError(t, err)

	got, err := svc.Get(context.Background(), owner, seeded.ID)
	require.NoError(t, err)
	assert.Equal(t, backlog.StatusClosed, got.Status)
}

func TestService_Close_RequiresMemberRole(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	viewer := q.SeedUser("viewer", "viewer@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, viewer, "viewer")
	seeded := q.SeedBacklog(p.ID, "Release 2.4")

	_, err := svc.Close(context.Background(), viewer, seeded.ID)
	assert.ErrorIs(t, err, backlog.ErrForbidden)
}

func TestService_Close_ReturnsNotFoundForMissingBacklog(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Close(context.Background(), owner, uuid.New())
	assert.ErrorIs(t, err, backlog.ErrNotFound)
}
