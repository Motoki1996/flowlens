package flowmetrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/flowmetrics"
	"github.com/flowlens/api/internal/mrsync"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture mirrors deliverymetrics' own: an owner, project, GitLab
// connection, linked GitLab project and repository already seeded, so a
// test only has to seed the tasks/events/merge requests it cares about.
type fixture struct {
	svc     *flowmetrics.Service
	q       *dbtest.FakeQuerier
	owner   uuid.UUID
	project db.Project
	repo    db.Repository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	q := dbtest.New()
	projects := project.NewService(q)
	svc := flowmetrics.NewService(q, projects)

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

// linkMergeRequest seeds a merge request opened at gitlabCreated (and
// merged at merged, if non-nil), linked to taskID.
func (f fixture) linkMergeRequest(t *testing.T, taskID uuid.UUID, n int64, gitlabCreated time.Time, merged *time.Time) db.MergeRequest {
	t.Helper()
	ctx := context.Background()
	mr, err := f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID:         f.repo.ID,
		GitlabMergeRequestID: n,
		Number:               int32(n),
		Title:                "MR",
		State:                "opened",
		GitlabCreatedAt:      pgtype.Timestamptz{Time: gitlabCreated, Valid: true},
	})
	require.NoError(t, err)

	mr, err = f.q.UpdateMergeRequestTaskID(ctx, db.UpdateMergeRequestTaskIDParams{
		ID:     mr.ID,
		TaskID: pgtype.UUID{Bytes: taskID, Valid: true},
	})
	require.NoError(t, err)

	if merged != nil {
		mr, err = f.q.UpdateMergeRequest(ctx, db.UpdateMergeRequestParams{
			GitlabMergeRequestID: n,
			Title:                mr.Title,
			State:                "merged",
			AuthorGitlabUsername: mr.AuthorGitlabUsername,
			AuthorAvatarUrl:      mr.AuthorAvatarUrl,
			BaseBranch:           mr.BaseBranch,
			HeadBranch:           mr.HeadBranch,
			GitlabUpdatedAt:      mr.GitlabUpdatedAt,
			MergedAt:             pgtype.Timestamptz{Time: *merged, Valid: true},
			ClosedAt:             mr.ClosedAt,
			HtmlUrl:              mr.HtmlUrl,
			PipelineStatus:       mr.PipelineStatus,
			PipelineID:           mr.PipelineID,
			PipelineUpdatedAt:    mr.PipelineUpdatedAt,
		})
		require.NoError(t, err)
	}
	return mr
}

func TestService_Compute_ForeignProjectGetsNotFound(t *testing.T) {
	f := newFixture(t)
	intruder := f.q.SeedUser("mallory", "mallory@example.com")

	_, err := f.svc.Compute(context.Background(), intruder.ID, f.project.ID, nil, nil)
	assert.ErrorIs(t, err, flowmetrics.ErrNotFound)
}

func TestService_Compute_NoData(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.WaitingToStart.Count)
	assert.Nil(t, got.WaitingToStart.Median)
	assert.Equal(t, 0, got.Implementation.Count)
	assert.Equal(t, 0, got.ReviewAndMerge.Count)
	assert.Equal(t, 0, got.Completion.Count)
	assert.Equal(t, 0, got.Blocked.Count)
}

func TestService_Compute_SingleTaskFullPipeline(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	startedWork := created.Add(3 * time.Hour)
	mrOpened := startedWork.Add(5 * time.Hour)
	merged := mrOpened.Add(2 * time.Hour)
	done := merged.Add(1 * time.Hour)

	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", startedWork)
	f.linkMergeRequest(t, task.ID, 1, mrOpened, &merged)
	f.q.SeedTaskProgressEvent(task.ID, "in_progress", "done", done)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.WaitingToStart.Count)
	assert.InDelta(t, 3.0, *got.WaitingToStart.Median, 0.001)

	require.Equal(t, 1, got.Implementation.Count)
	assert.InDelta(t, 5.0, *got.Implementation.Median, 0.001)

	require.Equal(t, 1, got.ReviewAndMerge.Count)
	assert.InDelta(t, 2.0, *got.ReviewAndMerge.Median, 0.001)

	require.Equal(t, 1, got.Completion.Count)
	assert.InDelta(t, 1.0, *got.Completion.Median, 0.001)

	assert.Equal(t, 0, got.Blocked.Count)
}

func TestService_Compute_MedianAndP90WithOutlier(t *testing.T) {
	f := newFixture(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Four quick starts (1h waiting) and one very slow outlier (100h).
	// Median should stay near the quick starts; p90 should reflect the
	// outlier.
	hours := []float64{1, 1, 1, 1, 100}
	for i, h := range hours {
		created := base.Add(time.Duration(i) * 24 * time.Hour)
		started := created.Add(time.Duration(h * float64(time.Hour)))
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
		f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", started)
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 5, got.WaitingToStart.Count)
	assert.InDelta(t, 1.0, *got.WaitingToStart.Median, 0.001)
	assert.InDelta(t, 100.0, *got.WaitingToStart.P90, 0.001)
}

func TestService_Compute_TaskWithoutMergeRequestExcludedFromImplementationAndReview(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	started := created.Add(2 * time.Hour)

	// A task with no code change: it reaches in_progress and done, but
	// never gets a linked merge request.
	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Docs-only task", created)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", started)
	f.q.SeedTaskProgressEvent(task.ID, "in_progress", "done", started.Add(1*time.Hour))

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.WaitingToStart.Count)
	assert.Equal(t, 0, got.Implementation.Count)
	assert.Equal(t, 0, got.ReviewAndMerge.Count)
	assert.Equal(t, 0, got.Completion.Count)
}

func TestService_Compute_BlockedSumsClosedOnHoldIntervalsAcrossRoundTrips(t *testing.T) {
	f := newFixture(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", base)

	// in_progress -> on_hold (2h) -> in_progress -> on_hold (3h, still
	// open: no exit event). Only the first, closed interval should count.
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", base.Add(1*time.Hour))
	f.q.SeedTaskProgressEvent(task.ID, "in_progress", "on_hold", base.Add(2*time.Hour))
	f.q.SeedTaskProgressEvent(task.ID, "on_hold", "in_progress", base.Add(4*time.Hour))
	f.q.SeedTaskProgressEvent(task.ID, "in_progress", "on_hold", base.Add(10*time.Hour))

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.Blocked.Count)
	assert.InDelta(t, 2.0, *got.Blocked.Median, 0.001)
}

func TestService_Compute_TaskNeverOnHoldExcludedFromBlocked(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", created.Add(1*time.Hour))

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.Blocked.Count)
}

func TestService_Compute_FiltersBySinceUntil(t *testing.T) {
	f := newFixture(t)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	oldTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Old", old)
	f.q.SeedTaskProgressEvent(oldTask.ID, "not_started", "in_progress", old.Add(1*time.Hour))
	recentTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Recent", recent)
	f.q.SeedTaskProgressEvent(recentTask.ID, "not_started", "in_progress", recent.Add(2*time.Hour))

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &since, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.WaitingToStart.Count)
	assert.InDelta(t, 2.0, *got.WaitingToStart.Median, 0.001)
}
