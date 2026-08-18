package deliverymetrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/deliverymetrics"
	"github.com/flowlens/api/internal/metricsperiod"
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

	_, err := f.svc.Compute(context.Background(), intruder.ID, f.project.ID, nil, nil, nil)
	assert.ErrorIs(t, err, deliverymetrics.ErrNotFound)
}

func TestService_Compute_NoData(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
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

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
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

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 5, got.OpenToFirstReview.Count)
	assert.InDelta(t, 1.0, *got.OpenToFirstReview.Median, 0.001)
	assert.InDelta(t, 100.0, *got.OpenToFirstReview.P90, 0.001)
}

func TestService_Compute_ExcludesUnreviewedFromOpenToFirstReview(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "opened", created, nil, nil, "")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
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

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
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
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &since, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.Throughput)
}

// Bucketing (issue #188). interval=nil is exercised by every test above;
// these cover the periods behavior specifically.

func TestService_Compute_IntervalNil_LeavesPeriodsEmpty(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "merged", created, nil, nil, "")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Nil(t, got.Interval)
	assert.False(t, got.Truncated)
	assert.Empty(t, got.Periods)
}

func TestService_Compute_IntervalMonth_SplitsAcrossMonthBoundary(t *testing.T) {
	f := newFixture(t)
	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "merged", jan, nil, nil, "")
	f.createMergeRequest(t, 2, "merged", feb, nil, nil, "")

	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &month)
	require.NoError(t, err)

	require.Equal(t, &month, got.Interval)
	require.Len(t, got.Periods, 2)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[0].Start)
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), got.Periods[0].End)
	assert.Equal(t, 1, got.Periods[0].Throughput)
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), got.Periods[1].Start)
	assert.Equal(t, 1, got.Periods[1].Throughput)
	assert.Equal(t, 2, got.Throughput, "the overall total is unaffected by bucketing")
}

func TestService_Compute_IntervalYear_SplitsAcrossYearBoundary(t *testing.T) {
	f := newFixture(t)
	f.createMergeRequest(t, 1, "merged", time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), nil, nil, "")
	f.createMergeRequest(t, 2, "merged", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil, nil, "")

	year := metricsperiod.Year
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &year)
	require.NoError(t, err)

	require.Len(t, got.Periods, 2)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[0].Start)
	assert.Equal(t, 1, got.Periods[0].Throughput)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[1].Start)
	assert.Equal(t, 1, got.Periods[1].Throughput)
}

func TestService_Compute_IntervalWeek_SundayFallsInPreviousMondayWeek(t *testing.T) {
	f := newFixture(t)
	// 2026-03-15 is a Sunday; its ISO week starts Monday 2026-03-09.
	sunday := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "merged", sunday, nil, nil, "")

	week := metricsperiod.Week
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &week)
	require.NoError(t, err)

	require.Len(t, got.Periods, 1)
	assert.Equal(t, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), got.Periods[0].Start)
	assert.Equal(t, 1, got.Periods[0].Throughput)
}

func TestService_Compute_EmptyPeriodsAreZeroFilled(t *testing.T) {
	f := newFixture(t)
	f.createMergeRequest(t, 1, "merged", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), nil, nil, "")
	f.createMergeRequest(t, 2, "merged", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), nil, nil, "")

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &from, &to, &month)
	require.NoError(t, err)

	require.Len(t, got.Periods, 3)
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), got.Periods[1].Start)
	assert.Equal(t, 0, got.Periods[1].Throughput)
	assert.Equal(t, 0, got.Periods[1].OpenToFirstReview.Count)
	assert.Nil(t, got.Periods[1].OpenToFirstReview.Median)
}

func TestService_Compute_OverCapTruncatesToNewestAndReportsTruncated(t *testing.T) {
	f := newFixture(t)
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) // 85 months apart, over the 52 cap

	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &from, &to, &month)
	require.NoError(t, err)

	require.Len(t, got.Periods, metricsperiod.MaxPeriods)
	assert.True(t, got.Truncated)
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[len(got.Periods)-1].Start)
}

func TestService_Compute_PeriodMedianP90UsesOnlyThatPeriodsSamples(t *testing.T) {
	f := newFixture(t)
	// January: two fast reviews (1h). March: one very slow review (100h).
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "opened", jan1, timePtr(jan1.Add(1*time.Hour)), nil, "")
	f.createMergeRequest(t, 2, "opened", jan2, timePtr(jan2.Add(1*time.Hour)), nil, "")
	f.createMergeRequest(t, 3, "opened", mar, timePtr(mar.Add(100*time.Hour)), nil, "")

	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &month)
	require.NoError(t, err)

	require.Len(t, got.Periods, 3)
	require.Equal(t, 2, got.Periods[0].OpenToFirstReview.Count)
	assert.InDelta(t, 1.0, *got.Periods[0].OpenToFirstReview.Median, 0.001)
	require.Equal(t, 1, got.Periods[2].OpenToFirstReview.Count)
	assert.InDelta(t, 100.0, *got.Periods[2].OpenToFirstReview.Median, 0.001)
}

func timePtr(t time.Time) *time.Time { return &t }

func TestService_Compute_ThroughputCountsOnlyMergedState(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.createMergeRequest(t, 1, "merged", created, nil, nil, "")
	f.createMergeRequest(t, 2, "opened", created, nil, nil, "")
	f.createMergeRequest(t, 3, "closed", created, nil, nil, "")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.Throughput)
}
