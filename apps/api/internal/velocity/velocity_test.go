package velocity_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/metricsperiod"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/velocity"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	svc     *velocity.Service
	q       *dbtest.FakeQuerier
	owner   uuid.UUID
	project db.Project
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	q := dbtest.New()
	projects := project.NewService(q)
	svc := velocity.NewService(q, projects)

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	return fixture{svc: svc, q: q, owner: owner.ID, project: p}
}

func TestService_Compute_ForeignProjectGetsNotFound(t *testing.T) {
	f := newFixture(t)
	intruder := f.q.SeedUser("mallory", "mallory@example.com")

	_, err := f.svc.Compute(context.Background(), intruder.ID, f.project.ID, nil, nil, metricsperiod.Week)
	assert.ErrorIs(t, err, velocity.ErrNotFound)
}

func TestService_Compute_NoData(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	assert.Empty(t, got.Periods)
	assert.False(t, got.Truncated)
	assert.Equal(t, 0, got.OpenTaskCount)
	assert.Nil(t, got.AverageVelocity)
	assert.Nil(t, got.ForecastPeriods)
}

// A task counts toward exactly one bucket, and its completion time (not its
// created_at) decides which. Table-driven over the ways a task can resolve
// to "completed" (issue #195's "完了の定義" section).
func TestService_Compute_CompletionResolution(t *testing.T) {
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC) // a Monday

	tests := []struct {
		name          string
		seed          func(f fixture, taskID uuid.UUID)
		wantCompleted int
		wantUser      int
		wantAgent     int
		wantUnknown   int
	}{
		{
			name: "progress done only, agent actor",
			seed: func(f fixture, taskID uuid.UUID) {
				f.q.SeedTaskProgressEventWithActor(taskID, "in_progress", "done", base, "agent")
			},
			wantCompleted: 1,
			wantAgent:     1,
		},
		{
			name: "progress done only, user actor",
			seed: func(f fixture, taskID uuid.UUID) {
				f.q.SeedTaskProgressEventWithActor(taskID, "in_progress", "done", base, "user")
			},
			wantCompleted: 1,
			wantUser:      1,
		},
		{
			name:          "closed_at only (GitLab-side close), no progress event",
			seed:          func(f fixture, taskID uuid.UUID) { f.q.SeedTaskClosedAt(taskID, base) },
			wantCompleted: 1,
			wantUnknown:   1,
		},
		{
			name: "both present, closed_at earlier decides completion and actor is unknown",
			seed: func(f fixture, taskID uuid.UUID) {
				f.q.SeedTaskClosedAt(taskID, base)
				f.q.SeedTaskProgressEventWithActor(taskID, "in_progress", "done", base.Add(2*time.Hour), "agent")
			},
			wantCompleted: 1,
			wantUnknown:   1,
		},
		{
			name: "both present, done event earlier decides completion and its actor is used",
			seed: func(f fixture, taskID uuid.UUID) {
				f.q.SeedTaskProgressEventWithActor(taskID, "in_progress", "done", base, "user")
				f.q.SeedTaskClosedAt(taskID, base.Add(2*time.Hour))
			},
			wantCompleted: 1,
			wantUser:      1,
		},
		{
			name: "reopened and redone counts once, at the first done transition",
			seed: func(f fixture, taskID uuid.UUID) {
				f.q.SeedTaskProgressEventWithActor(taskID, "in_progress", "done", base, "user")
				f.q.SeedTaskProgressEventWithActor(taskID, "done", "in_progress", base.Add(1*time.Hour), "user")
				f.q.SeedTaskProgressEventWithActor(taskID, "in_progress", "done", base.Add(2*time.Hour), "agent")
			},
			wantCompleted: 1,
			wantUser:      1,
		},
		{
			name:          "neither signal present, task not counted at all",
			seed:          func(f fixture, taskID uuid.UUID) {},
			wantCompleted: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", base.Add(-30*24*time.Hour))
			tt.seed(f, task.ID)

			got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
			require.NoError(t, err)

			if tt.wantCompleted == 0 {
				assert.Empty(t, got.Periods)
				return
			}
			require.Len(t, got.Periods, 1)
			p := got.Periods[0]
			assert.Equal(t, tt.wantCompleted, p.Completed)
			assert.Equal(t, tt.wantUser, p.CompletedByUser)
			assert.Equal(t, tt.wantAgent, p.CompletedByAgent)
			assert.Equal(t, tt.wantUnknown, p.CompletedByUnknown)
			assert.Equal(t, p.Completed, p.CompletedByUser+p.CompletedByAgent+p.CompletedByUnknown)
		})
	}
}

// from/to bound each task's resolved completion time, not tasks.created_at
// — the opposite cohort basis from flowmetrics/deliverymetrics, and the
// detail issue #195 calls out as the one most likely to be implemented
// backwards.
func TestService_Compute_FromToBoundsCompletionTimeNotCreatedAt(t *testing.T) {
	f := newFixture(t)
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	completedInRange := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	completedOutOfRange := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	inRangeTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "In range", longAgo)
	f.q.SeedTaskProgressEventWithActor(inRangeTask.ID, "in_progress", "done", completedInRange, "agent")

	outOfRangeTask := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Out of range", longAgo)
	f.q.SeedTaskProgressEventWithActor(outOfRangeTask.ID, "in_progress", "done", completedOutOfRange, "agent")

	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, &from, &to, metricsperiod.Week)
	require.NoError(t, err)

	total := 0
	for _, p := range got.Periods {
		total += p.Completed
	}
	assert.Equal(t, 1, total)
}

// A period with no completions still appears, at count 0, between two
// periods that do — issue #195's gap-fill requirement.
func TestService_Compute_GapFilledEmptyPeriod(t *testing.T) {
	f := newFixture(t)
	week1 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // Monday
	week3 := week1.AddDate(0, 0, 14)                     // two weeks later, week1's bucket + 2

	t1 := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "T1", week1)
	f.q.SeedTaskProgressEventWithActor(t1.ID, "in_progress", "done", week1, "agent")
	t2 := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "T2", week3)
	f.q.SeedTaskProgressEventWithActor(t2.ID, "in_progress", "done", week3, "agent")

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	require.Len(t, got.Periods, 3)
	assert.Equal(t, 1, got.Periods[0].Completed)
	assert.Equal(t, 0, got.Periods[1].Completed)
	assert.Equal(t, 1, got.Periods[2].Completed)
}

// AverageVelocity must exclude a still-running period from its window —
// otherwise the current (necessarily partial) period drags the average
// down and understates real throughput. Regression test for issue #195.
func TestService_Compute_AverageVelocityExcludesInProgressPeriod(t *testing.T) {
	f := newFixture(t)
	currentWeekStart := metricsperiod.BucketStart(time.Now(), metricsperiod.Week)

	// Four complete, fully-past weeks with known counts (2,3,4,5 tasks) plus
	// an older fifth week (10 tasks) that should fall outside the
	// MovingAverageWindow=4 lookback.
	weekCounts := []int{10, 2, 3, 4, 5} // oldest to newest, excluding current
	for i, n := range weekCounts {
		weeksAgo := len(weekCounts) - i // 5,4,3,2,1 weeks ago
		weekStart := currentWeekStart.AddDate(0, 0, -7*weeksAgo)
		for j := 0; j < n; j++ {
			task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", weekStart)
			f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", weekStart.Add(time.Hour), "agent")
		}
	}

	// The current, still-running week: a huge count that must NOT affect
	// AverageVelocity.
	for j := 0; j < 100; j++ {
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", currentWeekStart)
		f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", currentWeekStart.Add(time.Minute), "agent")
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	require.NotNil(t, got.AverageVelocity)
	// (2+3+4+5)/4 = 3.5, excluding both the current partial week and the
	// too-old fifth week.
	assert.InDelta(t, 3.5, *got.AverageVelocity, 0.001)

	last := got.Periods[len(got.Periods)-1]
	assert.False(t, last.Complete)
}

func TestService_Compute_ForecastPeriods(t *testing.T) {
	f := newFixture(t)
	weekStart := metricsperiod.BucketStart(time.Now().AddDate(0, 0, -14), metricsperiod.Week)
	for j := 0; j < 4; j++ {
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", weekStart)
		f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", weekStart.Add(time.Hour), "agent")
		f.q.SeedTaskProgress(task.ID, "done")
	}
	for j := 0; j < 8; j++ {
		t := f.q.SeedTask(f.project.ID, f.owner, "Open")
		f.q.SeedTaskProgress(t.ID, "in_progress")
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	assert.Equal(t, 8, got.OpenTaskCount)
	require.NotNil(t, got.AverageVelocity)
	require.NotNil(t, got.ForecastPeriods)
	assert.InDelta(t, float64(got.OpenTaskCount)/(*got.AverageVelocity), *got.ForecastPeriods, 0.001)
}

// Points weight each completed task by its size, and the actor split of the
// points always sums back to CompletedPoints exactly as the counts do.
// Table-driven over the five sizes so the weight table itself is pinned:
// a silent edit to sizePoints has to break a named case here.
func TestService_Compute_CompletedPointsWeightBySize(t *testing.T) {
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC) // a Monday

	tests := []struct {
		name       string
		size       string
		wantPoints int
	}{
		{name: "xs weighs 1", size: "xs", wantPoints: 1},
		{name: "s weighs 2", size: "s", wantPoints: 2},
		{name: "m weighs 3 (the default)", size: "m", wantPoints: 3},
		{name: "l weighs 5", size: "l", wantPoints: 5},
		{name: "xl weighs 8", size: "xl", wantPoints: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", base.Add(-30*24*time.Hour))
			f.q.SeedTaskSize(task.ID, tt.size)
			f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", base, "agent")

			got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
			require.NoError(t, err)

			require.Len(t, got.Periods, 1)
			p := got.Periods[0]
			assert.Equal(t, 1, p.Completed, "one task regardless of size")
			assert.Equal(t, tt.wantPoints, p.CompletedPoints)
			assert.Equal(t, tt.wantPoints, p.CompletedPointsByAgent)
			assert.Equal(t, p.CompletedPoints,
				p.CompletedPointsByUser+p.CompletedPointsByAgent+p.CompletedPointsByUnknown)
		})
	}
}

// The point split follows the same actor attribution as the counts: only a
// done transition that actually decided the completion time carries an
// actor, so a closed_at-only completion lands in Unknown for points too.
func TestService_Compute_PointsActorSplitMatchesCountSplit(t *testing.T) {
	f := newFixture(t)
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	created := base.Add(-30 * 24 * time.Hour)

	// xl by user (8), l by agent (5), s closed on GitLab's side (2, unknown).
	byUser := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "User task", created)
	f.q.SeedTaskSize(byUser.ID, "xl")
	f.q.SeedTaskProgressEventWithActor(byUser.ID, "in_progress", "done", base, "user")

	byAgent := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Agent task", created)
	f.q.SeedTaskSize(byAgent.ID, "l")
	f.q.SeedTaskProgressEventWithActor(byAgent.ID, "in_progress", "done", base, "agent")

	unknown := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "GitLab-closed task", created)
	f.q.SeedTaskSize(unknown.ID, "s")
	f.q.SeedTaskClosedAt(unknown.ID, base)

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	require.Len(t, got.Periods, 1)
	p := got.Periods[0]
	assert.Equal(t, 3, p.Completed)
	assert.Equal(t, 15, p.CompletedPoints, "8 + 5 + 2")
	assert.Equal(t, 8, p.CompletedPointsByUser)
	assert.Equal(t, 5, p.CompletedPointsByAgent)
	assert.Equal(t, 2, p.CompletedPointsByUnknown)
	assert.Equal(t, p.CompletedPoints,
		p.CompletedPointsByUser+p.CompletedPointsByAgent+p.CompletedPointsByUnknown)
}

// The point-denominated average has to exclude a still-running period for
// exactly the reason the count-denominated one does — a partial bucket is
// low by construction. This is the regression issue #195 named as the
// easiest thing to get wrong, now doubled.
func TestService_Compute_AverageVelocityPointsExcludesInProgressPeriod(t *testing.T) {
	f := newFixture(t)
	currentWeekStart := metricsperiod.BucketStart(time.Now(), metricsperiod.Week)

	// Two complete weeks, one 'l' task (5 points) each.
	for weeksAgo := 1; weeksAgo <= 2; weeksAgo++ {
		weekStart := currentWeekStart.AddDate(0, 0, -7*weeksAgo)
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", weekStart)
		f.q.SeedTaskSize(task.ID, "l")
		f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", weekStart.Add(time.Hour), "agent")
	}

	// The current, still-running week: a pile of xs tasks that must not drag
	// the point average down.
	for j := 0; j < 20; j++ {
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", currentWeekStart)
		f.q.SeedTaskSize(task.ID, "xs")
		f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", currentWeekStart.Add(time.Minute), "agent")
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	require.NotNil(t, got.AverageVelocityPoints)
	assert.InDelta(t, 5.0, *got.AverageVelocityPoints, 0.001, "(5+5)/2, current week excluded")

	last := got.Periods[len(got.Periods)-1]
	assert.False(t, last.Complete)
	assert.Equal(t, 20, last.CompletedPoints, "the partial week is still reported, just not averaged")
}

// OpenTaskPoints weights the remaining open tasks the same way, and the
// point forecast divides one by the other.
func TestService_Compute_ForecastPeriodsByPoints(t *testing.T) {
	f := newFixture(t)
	weekStart := metricsperiod.BucketStart(time.Now().AddDate(0, 0, -14), metricsperiod.Week)

	// Two completed 'm' tasks (3 points each) in one complete week.
	for j := 0; j < 2; j++ {
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", weekStart)
		f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", weekStart.Add(time.Hour), "agent")
		f.q.SeedTaskProgress(task.ID, "done")
	}
	// Three open tasks: xl (8) + m (3) + xs (1) = 12 points.
	for _, size := range []string{"xl", "m", "xs"} {
		open := f.q.SeedTask(f.project.ID, f.owner, "Open")
		f.q.SeedTaskSize(open.ID, size)
		f.q.SeedTaskProgress(open.ID, "in_progress")
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	assert.Equal(t, 3, got.OpenTaskCount)
	assert.Equal(t, 12, got.OpenTaskPoints)
	require.NotNil(t, got.AverageVelocityPoints)
	assert.InDelta(t, 6.0, *got.AverageVelocityPoints, 0.001, "two 'm' tasks = 6 points in one week")
	require.NotNil(t, got.ForecastPeriodsByPoints)
	assert.InDelta(t, 2.0, *got.ForecastPeriodsByPoints, 0.001, "12 points / 6 per week")

	// The count forecast disagrees here on purpose: 3 open tasks / 2 per
	// week = 1.5 weeks, which understates the work because the remaining
	// tasks are larger than average. That divergence is the whole reason
	// sizing exists.
	require.NotNil(t, got.ForecastPeriods)
	assert.InDelta(t, 1.5, *got.ForecastPeriods, 0.001)
}

// MovingAveragePoints smooths the same window MovingAverage does, over
// points instead of counts.
func TestService_Compute_MovingAveragePoints(t *testing.T) {
	f := newFixture(t)
	currentWeekStart := metricsperiod.BucketStart(time.Now(), metricsperiod.Week)

	// Three consecutive complete weeks of one task each: xs (1), l (5), xl (8).
	sizes := []string{"xs", "l", "xl"}
	for i, size := range sizes {
		weeksAgo := len(sizes) - i // 3, 2, 1
		weekStart := currentWeekStart.AddDate(0, 0, -7*weeksAgo)
		task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", weekStart)
		f.q.SeedTaskSize(task.ID, size)
		f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", weekStart.Add(time.Hour), "agent")
	}

	got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(got.Periods), 3)
	assert.InDelta(t, 1.0, got.Periods[0].MovingAveragePoints, 0.001, "1/1")
	assert.InDelta(t, 3.0, got.Periods[1].MovingAveragePoints, 0.001, "(1+5)/2")
	assert.InDelta(t, 14.0/3.0, got.Periods[2].MovingAveragePoints, 0.001, "(1+5+8)/3")
}

// SizedTaskRatio tells a caller whether the point series carries any real
// information yet: every task predating migration 000025 reads as the
// default 'm', which would make points a flat 3x copy of the counts.
func TestService_Compute_SizedTaskRatio(t *testing.T) {
	base := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	created := base.Add(-30 * 24 * time.Hour)

	tests := []struct {
		name      string
		sizes     []string
		wantRatio float64
	}{
		{name: "no completed tasks at all", sizes: nil, wantRatio: 0},
		{name: "every task still at the default", sizes: []string{"m", "m", "m"}, wantRatio: 0},
		{name: "half sized", sizes: []string{"m", "m", "l", "xs"}, wantRatio: 0.5},
		{name: "all sized", sizes: []string{"xs", "l", "xl"}, wantRatio: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			for _, size := range tt.sizes {
				task := f.q.SeedTaskWithCreatedAt(f.project.ID, f.owner, "Task", created)
				f.q.SeedTaskSize(task.ID, size)
				f.q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", base, "agent")
			}

			got, err := f.svc.Compute(context.Background(), f.owner, f.project.ID, nil, nil, metricsperiod.Week)
			require.NoError(t, err)
			assert.InDelta(t, tt.wantRatio, got.SizedTaskRatio, 0.001)
		})
	}
}
