package backlog_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/optional"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *backlog.Service {
	return backlog.NewService(q, project.NewService(q))
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

func TestService_Create_AppendsToEndOfPosition(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	ctx := context.Background()

	first, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	second, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 2"})
	require.NoError(t, err)

	assert.Equal(t, int32(0), first.Position)
	assert.Equal(t, int32(1), second.Position)
}

func TestService_List_ScopesToProjectAndOrdersByPosition(t *testing.T) {
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

	backlogs, err := svc.List(ctx, owner, p.ID)
	require.NoError(t, err)
	require.Len(t, backlogs, 2)
	assert.Equal(t, "Sprint 1", backlogs[0].Name)
	assert.Equal(t, "Sprint 2", backlogs[1].Name)
}

func TestService_List_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.List(context.Background(), other, p.ID)
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

		_, err := svc.Update(ctx, other, b.ID, backlog.UpdateParams{Name: "Hijacked"})
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

func TestService_Update_ChangesNameDescriptionAndPosition(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")

	updated, err := svc.Update(context.Background(), owner, b.ID, backlog.UpdateParams{
		Name:        "Renamed",
		Description: "new description",
		Position:    5,
	})
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, "new description", updated.Description)
	assert.Equal(t, int32(5), updated.Position)
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

	updated, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Renamed"})
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
	})
	require.NoError(t, err)
	assert.Equal(t, date(2026, 8, 1), set.StartDate)

	cleared, err := svc.Update(ctx, owner, b.ID, backlog.UpdateParams{
		Name:      "Sprint 1",
		StartDate: optional.Present[*time.Time](nil),
	})
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
	})
	assert.ErrorIs(t, err, backlog.ErrInvalidSchedule)

	unchanged, err := svc.Get(ctx, owner, created.ID)
	require.NoError(t, err)
	assert.Nil(t, unchanged.StartDate)
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
