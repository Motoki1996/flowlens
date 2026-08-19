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
