package deliverymetrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/deliverymetrics"
	"github.com/flowlens/api/internal/mrsync"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture mirrors the mergerequest package's own: an owner, project,
// GitLab connection, linked GitLab project and repository already seeded.
type fixture struct {
	svc     *deliverymetrics.Service
	q       *dbtest.FakeQuerier
	owner   uuid.UUID
	project db.Project
	repo    db.Repository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	q := dbtest.New()
	projects := project.NewService(q)
	svc := deliverymetrics.NewService(q, projects)

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

// createMergeRequest seeds a merge request with the given creation/review/
// merge timestamps and pipeline status, defaulting number/gitlabID to n.
func (f fixture) createMergeRequest(t *testing.T, n int64, state string, created time.Time, firstReviewed, merged *time.Time, pipelineStatus string) db.MergeRequest {
	t.Helper()
	ctx := context.Background()
	mr, err := f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID:         f.repo.ID,
		GitlabMergeRequestID: n,
		Number:               int32(n),
		Title:                "MR",
		State:                state,
		GitlabCreatedAt:      pgtype.Timestamptz{Time: created, Valid: true},
		PipelineStatus:       pipelineStatus,
	})
	require.NoError(t, err)

	if firstReviewed != nil {
		mr, err = f.q.UpdateMergeRequestFirstReviewedAt(ctx, db.UpdateMergeRequestFirstReviewedAtParams{
			ID: mr.ID, FirstReviewedAt: pgtype.Timestamptz{Time: *firstReviewed, Valid: true},
		})
		require.NoError(t, err)
	}
	if merged != nil {
		mr, err = f.q.UpdateMergeRequest(ctx, db.UpdateMergeRequestParams{
			GitlabMergeRequestID: n,
			Title:                mr.Title,
			State:                mr.State,
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
	assert.ErrorIs(t, err, deliverymetrics.ErrNotFound)
}

func TestService_Compute_NoData(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.OpenToFirstReview.Count)
	assert.Nil(t, got.OpenToFirstReview.Median)
	assert.Equal(t, 0, got.FirstReviewToMerge.Count)
	assert.Nil(t, got.PipelineSuccessRate)
	assert.Equal(t, 0, got.Throughput)
}

func TestService_Compute_SingleMergeRequest(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reviewed := created.Add(2 * time.Hour)
	merged := reviewed.Add(1 * time.Hour)
	f.createMergeRequest(t, 1, "merged", created, &reviewed, &merged, "success")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.OpenToFirstReview.Count)
	assert.InDelta(t, 2.0, *got.OpenToFirstReview.Median, 0.001)
	assert.InDelta(t, 2.0, *got.OpenToFirstReview.P90, 0.001)

	require.Equal(t, 1, got.FirstReviewToMerge.Count)
	assert.InDelta(t, 1.0, *got.FirstReviewToMerge.Median, 0.001)

	require.NotNil(t, got.PipelineSuccessRate)
	assert.InDelta(t, 1.0, *got.PipelineSuccessRate, 0.001)
	assert.Equal(t, 1, got.Throughput)
}

func TestService_Compute_MedianAndP90WithOutlier(t *testing.T) {
	f := newFixture(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Four quick reviews (1h) and one very slow outlier (100h). Median
	// should stay near the quick reviews; p90 should reflect the outlier.
	hours := []float64{1, 1, 1, 1, 100}
	for i, h := range hours {
		created := base.Add(time.Duration(i) * 24 * time.Hour)
		reviewed := created.Add(time.Duration(h * float64(time.Hour)))
		f.createMergeRequest(t, int64(i+1), "opened", created, &reviewed, nil, "")
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 5, got.OpenToFirstReview.Count)
	assert.InDelta(t, 1.0, *got.OpenToFirstReview.Median, 0.001)
	assert.InDelta(t, 100.0, *got.OpenToFirstReview.P90, 0.001)
}

func TestService_Compute_ExcludesUnreviewedFromOpenToFirstReview(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "opened", created, nil, nil, "")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.OpenToFirstReview.Count)
}

func TestService_Compute_PipelineSuccessRateIgnoresUndecidedStatuses(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "opened", created, nil, nil, "success")
	f.createMergeRequest(t, 2, "opened", created, nil, nil, "failed")
	f.createMergeRequest(t, 3, "opened", created, nil, nil, "running")
	f.createMergeRequest(t, 4, "opened", created, nil, nil, "")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	require.NotNil(t, got.PipelineSuccessRate)
	assert.InDelta(t, 0.5, *got.PipelineSuccessRate, 0.001)
}

func TestService_Compute_FiltersBySinceUntil(t *testing.T) {
	f := newFixture(t)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "merged", old, nil, nil, "")
	f.createMergeRequest(t, 2, "merged", recent, nil, nil, "")

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &since, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.Throughput)
}

func TestService_Compute_ThroughputCountsOnlyMergedState(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "merged", created, nil, nil, "")
	f.createMergeRequest(t, 2, "opened", created, nil, nil, "")
	f.createMergeRequest(t, 3, "closed", created, nil, nil, "")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.Throughput)
}
