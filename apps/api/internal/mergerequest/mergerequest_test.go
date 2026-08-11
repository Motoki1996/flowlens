package mergerequest_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/mergerequest"
	"github.com/flowlens/api/internal/mrsync"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture bundles a mergerequest Service backed by an in-memory querier
// with an owner, project, GitLab connection, linked GitLab project and
// repository already seeded, mirroring internal/mrsync's own test fixture.
type fixture struct {
	svc     *mergerequest.Service
	q       *dbtest.FakeQuerier
	owner   uuid.UUID
	project db.Project
	repo    db.Repository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	q := dbtest.New()
	projects := project.NewService(q)
	svc := mergerequest.NewService(q, projects)

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, nil)

	ctx := context.Background()
	link, err := q.CreateLinkedGitlabProject(ctx, db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/project",
		Name:               "project",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)

	repo, err := mrsync.EnsureRepository(ctx, q, link)
	require.NoError(t, err)

	return fixture{svc: svc, q: q, owner: owner.ID, project: p, repo: repo}
}

func TestService_List_ForeignProjectGetsNotFound(t *testing.T) {
	f := newFixture(t)
	intruder := f.q.SeedUser("mallory", "mallory@example.com")

	_, err := f.svc.List(context.Background(), intruder.ID, f.project.ID, mergerequest.ListFilter{})
	assert.ErrorIs(t, err, mergerequest.ErrNotFound)
}

func TestService_List_FiltersByState(t *testing.T) {
	f := newFixture(t)
	f.q.SeedMergeRequest(f.repo.ID, 1, 1, "Opened one", "opened")
	f.q.SeedMergeRequest(f.repo.ID, 2, 2, "Merged one", "merged")

	got, err := f.svc.List(context.Background(), f.owner, f.project.ID, mergerequest.ListFilter{State: "merged"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Merged one", got[0].Title)
}

func TestService_List_FiltersByAuthor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: f.repo.ID, GitlabMergeRequestID: 1, Number: 1, Title: "Alice's", State: "opened",
		AuthorGitlabUsername: "alice",
	})
	require.NoError(t, err)
	_, err = f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: f.repo.ID, GitlabMergeRequestID: 2, Number: 2, Title: "Bob's", State: "opened",
		AuthorGitlabUsername: "bob",
	})
	require.NoError(t, err)

	got, err := f.svc.List(ctx, f.owner, f.project.ID, mergerequest.ListFilter{Author: "alice"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Alice's", got[0].Title)
}

func TestService_List_FiltersByTaskID(t *testing.T) {
	f := newFixture(t)
	task := f.q.SeedTask(f.project.ID, f.owner, "Fix the bug")
	linked := f.q.SeedMergeRequest(f.repo.ID, 1, 1, "Linked", "opened")
	_, err := f.q.UpdateMergeRequestTaskID(context.Background(), db.UpdateMergeRequestTaskIDParams{
		ID: linked.ID, TaskID: pgtype.UUID{Bytes: task.ID, Valid: true},
	})
	require.NoError(t, err)
	f.q.SeedMergeRequest(f.repo.ID, 2, 2, "Unlinked", "opened")

	taskID := task.ID
	got, err := f.svc.List(context.Background(), f.owner, f.project.ID, mergerequest.ListFilter{TaskID: &taskID})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Linked", got[0].Title)
}

func TestService_List_FiltersBySinceUntil(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: f.repo.ID, GitlabMergeRequestID: 1, Number: 1, Title: "Old", State: "opened",
		GitlabCreatedAt: pgtype.Timestamptz{Time: old, Valid: true},
	})
	require.NoError(t, err)
	_, err = f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: f.repo.ID, GitlabMergeRequestID: 2, Number: 2, Title: "Recent", State: "opened",
		GitlabCreatedAt: pgtype.Timestamptz{Time: recent, Valid: true},
	})
	require.NoError(t, err)

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := f.svc.List(ctx, f.owner, f.project.ID, mergerequest.ListFilter{Since: &since})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Recent", got[0].Title)
}

func TestService_List_SortsByUpdated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	older := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: f.repo.ID, GitlabMergeRequestID: 1, Number: 1, Title: "Created first, updated last", State: "opened",
		GitlabCreatedAt: pgtype.Timestamptz{Time: older, Valid: true},
		GitlabUpdatedAt: pgtype.Timestamptz{Time: newer, Valid: true},
	})
	require.NoError(t, err)
	_, err = f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: f.repo.ID, GitlabMergeRequestID: 2, Number: 2, Title: "Created last, updated first", State: "opened",
		GitlabCreatedAt: pgtype.Timestamptz{Time: newer, Valid: true},
		GitlabUpdatedAt: pgtype.Timestamptz{Time: older, Valid: true},
	})
	require.NoError(t, err)

	got, err := f.svc.List(ctx, f.owner, f.project.ID, mergerequest.ListFilter{Sort: mergerequest.SortUpdated})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Created first, updated last", got[0].Title)
}

func TestService_Get_ReturnsNotFoundForForeignProject(t *testing.T) {
	f := newFixture(t)
	mr := f.q.SeedMergeRequest(f.repo.ID, 1, 1, "Mine", "opened")
	intruder := f.q.SeedUser("mallory", "mallory@example.com")

	_, err := f.svc.Get(context.Background(), intruder.ID, mr.ID)
	assert.ErrorIs(t, err, mergerequest.ErrNotFound)
}

func TestService_Get_ReturnsMergeRequest(t *testing.T) {
	f := newFixture(t)
	mr := f.q.SeedMergeRequest(f.repo.ID, 1, 1, "Mine", "opened")

	got, err := f.svc.Get(context.Background(), f.owner, mr.ID)
	require.NoError(t, err)
	assert.Equal(t, "Mine", got.Title)
	assert.Equal(t, mr.ID, got.ID)
}

func TestService_ProjectID_ReturnsOwningProject(t *testing.T) {
	f := newFixture(t)
	mr := f.q.SeedMergeRequest(f.repo.ID, 1, 1, "Mine", "opened")

	got, err := f.svc.ProjectID(context.Background(), mr.ID)
	require.NoError(t, err)
	assert.Equal(t, f.project.ID, got)
}

func TestService_ProjectID_ReturnsNotFoundForUnknownID(t *testing.T) {
	f := newFixture(t)

	_, err := f.svc.ProjectID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, mergerequest.ErrNotFound)
}
