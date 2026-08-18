package flowmetrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/flowmetrics"
	"github.com/flowlens/api/internal/metricsperiod"
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

	_, err := f.svc.Compute(context.Background(), intruder.ID, f.project.ID, nil, nil, nil)
	assert.ErrorIs(t, err, flowmetrics.ErrNotFound)
}

func TestService_Compute_NoData(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.WaitingToStart.Count)
	assert.Nil(t, got.WaitingToStart.Median)
	assert.Equal(t, 0, got.Design.Count)
	assert.Equal(t, 0, got.Implementation.Count)
	assert.Equal(t, 0, got.ReviewAndMerge.Count)
	assert.Equal(t, 0, got.Completion.Count)
	assert.Equal(t, 0, got.Blocked.Count)
}

func TestService_Compute_SingleTaskFullPipeline(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	designStarted := created.Add(1 * time.Hour)
	startedWork := created.Add(3 * time.Hour)
	implementationStarted := startedWork
	mrOpened := implementationStarted.Add(5 * time.Hour)
	merged := mrOpened.Add(2 * time.Hour)
	done := merged.Add(1 * time.Hour)

	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", startedWork)
	f.q.SeedTaskDesignImplementationStarted(task.ID,
		pgtype.Timestamptz{Time: designStarted, Valid: true},
		pgtype.Timestamptz{Time: implementationStarted, Valid: true})
	f.linkMergeRequest(t, task.ID, 1, mrOpened, &merged)
	f.q.SeedTaskProgressEvent(task.ID, "in_progress", "done", done)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.WaitingToStart.Count)
	assert.InDelta(t, 3.0, *got.WaitingToStart.Median, 0.001)

	require.Equal(t, 1, got.Design.Count)
	assert.InDelta(t, 2.0, *got.Design.Median, 0.001)

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

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
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

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 1, got.WaitingToStart.Count)
	assert.Equal(t, 0, got.Implementation.Count)
	assert.Equal(t, 0, got.ReviewAndMerge.Count)
	assert.Equal(t, 0, got.Completion.Count)
}

// A task that calls .../implementation-started but never .../design-started
// (spec-driven development is optional, not mandatory) is excluded from
// Design entirely rather than counted as a zero-duration design phase — the
// same "both ends known" rule every other stage follows. Implementation is
// unaffected: it only needs implementation_started_at, not design_started_at.
func TestService_Compute_ImplementationStartedWithNoDesignStarted_ExcludedFromDesign(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	implementationStarted := created.Add(2 * time.Hour)
	mrOpened := implementationStarted.Add(3 * time.Hour)

	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
	f.q.SeedTaskDesignImplementationStarted(task.ID,
		pgtype.Timestamptz{},
		pgtype.Timestamptz{Time: implementationStarted, Valid: true})
	f.linkMergeRequest(t, task.ID, 1, mrOpened, nil)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.Design.Count)
	require.Equal(t, 1, got.Implementation.Count)
	assert.InDelta(t, 3.0, *got.Implementation.Median, 0.001)
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

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.Blocked.Count)
	assert.InDelta(t, 2.0, *got.Blocked.Median, 0.001)
}

func TestService_Compute_TaskNeverOnHoldExcludedFromBlocked(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", created.Add(1*time.Hour))

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
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
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &since, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.WaitingToStart.Count)
	assert.InDelta(t, 2.0, *got.WaitingToStart.Median, 0.001)
}

// Backlog-level stages (issue #173): backlogWaitingToStart
// (backlogs.created_at -> first in_progress) and taskBreakdown (first
// in_progress -> the earliest task filed under the backlog).

func TestService_Compute_BacklogWaitingToStart(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	startedWork := created.Add(6 * time.Hour)

	b := f.q.SeedBacklogWithCreatedAt(f.project.ID, "Sprint 1", created)
	f.q.SeedBacklogProgressEvent(b.ID, "not_started", "in_progress", startedWork)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.BacklogWaitingToStart.Count)
	assert.InDelta(t, 6.0, *got.BacklogWaitingToStart.Median, 0.001)
}

// A backlog that never went in_progress contributes to neither backlog
// stage — both ends unknown, the same "excluded, not zero" rule every other
// stage in this package follows.
func TestService_Compute_BacklogNeverStarted_ExcludedFromBacklogStages(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.q.SeedBacklogWithCreatedAt(f.project.ID, "Untouched", created)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.BacklogWaitingToStart.Count)
	assert.Equal(t, 0, got.TaskBreakdown.Count)
}

// The issue's first case: a backlog goes in_progress with no tasks filed
// under it yet, and a task appears afterwards — taskBreakdown measures the
// gap between the two.
func TestService_Compute_TaskBreakdown_TaskCreatedAfterInProgress(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	startedWork := created.Add(2 * time.Hour)
	firstTaskCreated := startedWork.Add(4 * time.Hour)

	b := f.q.SeedBacklogWithCreatedAt(f.project.ID, "Sprint 1", created)
	f.q.SeedBacklogProgressEvent(b.ID, "not_started", "in_progress", startedWork)
	f.q.SeedTaskInBacklogWithCreatedAt(f.project.ID, b.ID, f.owner, "Task 1", firstTaskCreated)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	require.Equal(t, 1, got.TaskBreakdown.Count)
	assert.InDelta(t, 4.0, *got.TaskBreakdown.Median, 0.001)
}

// The issue's second case: a backlog already has a task filed under it
// before it goes in_progress. There was no breakdown work to time after the
// transition, so the backlog is excluded from the stat entirely rather than
// counted as a zero (or negative) duration.
func TestService_Compute_TaskBreakdown_PreexistingTaskExcludesBacklog(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	taskCreated := created.Add(1 * time.Hour)
	startedWork := taskCreated.Add(1 * time.Hour)

	b := f.q.SeedBacklogWithCreatedAt(f.project.ID, "Sprint 1", created)
	f.q.SeedTaskInBacklogWithCreatedAt(f.project.ID, b.ID, f.owner, "Pre-existing task", taskCreated)
	f.q.SeedBacklogProgressEvent(b.ID, "not_started", "in_progress", startedWork)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.TaskBreakdown.Count)
	// backlogWaitingToStart is unaffected by the exclusion above — it's a
	// separate stage with its own endpoints.
	require.Equal(t, 1, got.BacklogWaitingToStart.Count)
}

// The issue's third case: a backlog goes in_progress and stays that way
// with zero tasks ever filed under it. Both ends of taskBreakdown are
// unknown, so it's excluded exactly like a task that never reached done.
func TestService_Compute_TaskBreakdown_NoTasksYet_Excluded(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	startedWork := created.Add(2 * time.Hour)

	b := f.q.SeedBacklogWithCreatedAt(f.project.ID, "Sprint 1", created)
	f.q.SeedBacklogProgressEvent(b.ID, "not_started", "in_progress", startedWork)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, got.TaskBreakdown.Count)
	require.Equal(t, 1, got.BacklogWaitingToStart.Count, "the backlog itself still reached in_progress")
}

// Bucketing (issue #188). interval=nil is exercised by every test above;
// these cover the periods behavior specifically.

func TestService_Compute_IntervalNil_LeavesPeriodsEmpty(t *testing.T) {
	f := newFixture(t)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", created.Add(1*time.Hour))

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
	janTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Jan task", jan)
	f.q.SeedTaskProgressEvent(janTask.ID, "not_started", "in_progress", jan.Add(2*time.Hour))
	febTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Feb task", feb)
	f.q.SeedTaskProgressEvent(febTask.ID, "not_started", "in_progress", feb.Add(4*time.Hour))

	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &month)
	require.NoError(t, err)

	require.Equal(t, &month, got.Interval)
	require.Len(t, got.Periods, 2)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[0].Start)
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), got.Periods[0].End)
	require.Equal(t, 1, got.Periods[0].WaitingToStart.Count)
	assert.InDelta(t, 2.0, *got.Periods[0].WaitingToStart.Median, 0.001)
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), got.Periods[1].Start)
	require.Equal(t, 1, got.Periods[1].WaitingToStart.Count)
	assert.InDelta(t, 4.0, *got.Periods[1].WaitingToStart.Median, 0.001)
	assert.Equal(t, 2, got.WaitingToStart.Count, "the overall total is unaffected by bucketing")
}

func TestService_Compute_IntervalYear_SplitsAcrossYearBoundary(t *testing.T) {
	f := newFixture(t)
	dec := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	decTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Dec task", dec)
	f.q.SeedTaskProgressEvent(decTask.ID, "not_started", "in_progress", dec.Add(1*time.Hour))
	janTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Jan task", jan)
	f.q.SeedTaskProgressEvent(janTask.ID, "not_started", "in_progress", jan.Add(1*time.Hour))

	year := metricsperiod.Year
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &year)
	require.NoError(t, err)

	require.Len(t, got.Periods, 2)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[0].Start)
	assert.Equal(t, 1, got.Periods[0].WaitingToStart.Count)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), got.Periods[1].Start)
	assert.Equal(t, 1, got.Periods[1].WaitingToStart.Count)
}

func TestService_Compute_IntervalWeek_SundayFallsInPreviousMondayWeek(t *testing.T) {
	f := newFixture(t)
	// 2026-03-15 is a Sunday; its ISO week starts Monday 2026-03-09.
	sunday := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", sunday)
	f.q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", sunday.Add(1*time.Hour))

	week := metricsperiod.Week
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &week)
	require.NoError(t, err)

	require.Len(t, got.Periods, 1)
	assert.Equal(t, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), got.Periods[0].Start)
	assert.Equal(t, 1, got.Periods[0].WaitingToStart.Count)
}

func TestService_Compute_EmptyPeriodsAreZeroFilled(t *testing.T) {
	f := newFixture(t)
	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	janTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Jan task", jan)
	f.q.SeedTaskProgressEvent(janTask.ID, "not_started", "in_progress", jan.Add(1*time.Hour))
	marTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Mar task", mar)
	f.q.SeedTaskProgressEvent(marTask.ID, "not_started", "in_progress", mar.Add(1*time.Hour))

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &from, &to, &month)
	require.NoError(t, err)

	require.Len(t, got.Periods, 3)
	assert.Equal(t, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), got.Periods[1].Start)
	assert.Equal(t, 0, got.Periods[1].WaitingToStart.Count)
	assert.Nil(t, got.Periods[1].WaitingToStart.Median)
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
	// January: two fast starts (1h). March: one very slow start (100h).
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t1 := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task 1", jan1)
	f.q.SeedTaskProgressEvent(t1.ID, "not_started", "in_progress", jan1.Add(1*time.Hour))
	t2 := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task 2", jan2)
	f.q.SeedTaskProgressEvent(t2.ID, "not_started", "in_progress", jan2.Add(1*time.Hour))
	t3 := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task 3", mar)
	f.q.SeedTaskProgressEvent(t3.ID, "not_started", "in_progress", mar.Add(100*time.Hour))

	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &month)
	require.NoError(t, err)

	require.Len(t, got.Periods, 3)
	require.Equal(t, 2, got.Periods[0].WaitingToStart.Count)
	assert.InDelta(t, 1.0, *got.Periods[0].WaitingToStart.Median, 0.001)
	require.Equal(t, 1, got.Periods[2].WaitingToStart.Count)
	assert.InDelta(t, 100.0, *got.Periods[2].WaitingToStart.Median, 0.001)
}

// Backlog-level stages (issue #173) bucket by backlogs.created_at, an
// independent cohort from the task stages above, but land in the same
// [Start, End) period.
func TestService_Compute_IntervalMonth_BacklogStagesBucketByBacklogCreatedAt(t *testing.T) {
	f := newFixture(t)
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	// Created in January but doesn't go in_progress until February — the
	// bucket it lands in should still be January's, per the created-at
	// cohort rule (issue #188), not the month the transition happened in.
	b := f.q.SeedBacklogWithCreatedAt(f.project.ID, "Sprint 1", jan)
	f.q.SeedBacklogProgressEvent(b.ID, "not_started", "in_progress", feb)
	// A second backlog, created in February, forces the timeline to span
	// both months so the assertion below is meaningful.
	b2 := f.q.SeedBacklogWithCreatedAt(f.project.ID, "Sprint 2", feb)
	f.q.SeedBacklogProgressEvent(b2.ID, "not_started", "in_progress", feb.Add(1*time.Hour))

	month := metricsperiod.Month
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, &month)
	require.NoError(t, err)

	require.Len(t, got.Periods, 2)
	require.Equal(t, 1, got.Periods[0].BacklogWaitingToStart.Count, "bucketed by the backlog's own created_at, not the in_progress transition")
	assert.InDelta(t, 31*24.0, *got.Periods[0].BacklogWaitingToStart.Median, 0.001)
	require.Equal(t, 1, got.Periods[1].BacklogWaitingToStart.Count)
}
