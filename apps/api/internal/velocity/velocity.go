// Package velocity computes throughput — completed tasks per period (issue
// #195) — as distinct from internal/deliverymetrics (merge-request lead
// time) and internal/flowmetrics (per-task stage lead time): both of those
// measure how long one item took, this measures how many items finished in
// a window. There is no story-point/estimate concept in FlowLens (see the
// issue for why), so velocity is a raw completed-task count, additionally
// split by task_progress_events.actor_kind into user/agent/unknown —
// "how much throughput did the agent actually produce" is a number no
// other tool here can give.
//
// A task's completion time is min(its first done-progress-transition's
// occurred_at, tasks.closed_at), whichever is non-nil; a task with neither
// is not completed. Both have to be checked: tasks.progress is app-only and
// never written by GitLab sync, so a task closed on the GitLab side alone
// never reaches progress='done' and would be invisible if only the
// task_progress_events log were read. Conversely tasks.status can stay
// 'open' after progress reaches 'done' (progress and status are separate
// axes that never write each other, per CLAUDE.md), so closed_at alone
// would miss those. Each task counts at most once, at the earlier of the
// two.
//
// task_progress_events only exists from migration 000020 on (issue #169);
// a task that reached done before that migration shipped has no event row
// and is only reachable via closed_at, with no actor breakdown — that gap
// cannot be backfilled and is expected, not a bug.
//
// Unlike flowmetrics/deliverymetrics, which bucket by each row's creation
// time, velocity buckets by completion time and from/to bound completion
// time too — this endpoint answers "how much finished in this window", so
// the cohort has to be "finished in this window", not "created in this
// window".
package velocity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/metricsperiod"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
)

// Sentinel errors returned by Service, matching flowmetrics/deliverymetrics'
// own so callers can share error-mapping code.
var (
	ErrNotFound  = errors.New("velocity: not found")
	ErrForbidden = errors.New("velocity: forbidden")
)

// MovingAverageWindow is how many trailing periods (this one included)
// Period.MovingAverage smooths over. A single period's completed count is
// too noisy on its own to act on; this is the number meant to actually be
// read.
const MovingAverageWindow = 4

// Period is one interval-sized bucket of completed-task counts.
type Period struct {
	// Start and End bound the bucket in UTC; End is exclusive.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`

	// Completed is the number of tasks whose completion time falls in
	// [Start, End). CompletedByUser + CompletedByAgent + CompletedByUnknown
	// always equals Completed.
	Completed int `json:"completed"`
	// CompletedByUser/Agent are attributed from the task_progress_events row
	// that decided the completion time — only when that row (the first
	// done transition) is the earlier of the two completion signals.
	CompletedByUser  int `json:"completedByUser"`
	CompletedByAgent int `json:"completedByAgent"`
	// CompletedByUnknown counts a task whose completion time came from
	// closed_at instead (a GitLab-side close, which carries no actor), or
	// which has no done transition at all.
	CompletedByUnknown int `json:"completedByUnknown"`

	// MovingAverage is the simple average of Completed over this period and
	// up to MovingAverageWindow-1 preceding periods (fewer if not that many
	// exist yet).
	MovingAverage float64 `json:"movingAverage"`
	// Complete is true once this bucket's End has passed — a still-running
	// period (typically the most recent one) is always partial and would
	// understate velocity if treated the same as a finished one.
	Complete bool `json:"complete"`
}

// Metrics is the aggregation Service.Compute returns.
type Metrics struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`

	Interval  metricsperiod.Interval `json:"interval"`
	Truncated bool                   `json:"truncated"`
	Periods   []Period               `json:"periods"`

	// OpenTaskCount is projectID's current count of not-yet-completed tasks
	// (status='open' AND progress<>'done'), regardless of from/to — it feeds
	// ForecastPeriods, which is about what's left right now, not about the
	// requested window.
	OpenTaskCount int `json:"openTaskCount"`
	// AverageVelocity is the mean Completed over the most recent (up to)
	// MovingAverageWindow periods with Complete=true, excluding any
	// still-running period — an in-progress period is necessarily
	// undercounted and would bias this down. nil when no period in range is
	// yet complete.
	AverageVelocity *float64 `json:"averageVelocity"`
	// ForecastPeriods is OpenTaskCount / AverageVelocity: how many more
	// periods, at the recent pace, the remaining open tasks would take. nil
	// whenever AverageVelocity is nil or zero.
	ForecastPeriods *float64 `json:"forecastPeriods"`
}

// Service is the velocity domain service. Read-only: there is no write path
// here at all.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a velocity Service. projects verifies project
// membership before any read.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

// completion is one task's resolved completion time and the actor it's
// attributed to, or ok=false if the task hasn't completed at all.
type completion struct {
	at    time.Time
	actor string // "user", "agent", or "" for unknown
}

// resolveCompletion applies the min(done occurred_at, closed_at) rule: the
// earlier of the two non-nil signals wins, and the actor is only trusted
// when the done transition is the one that decided it. A tie also prefers
// the done transition, since it carries strictly more information (an
// actor) than closed_at ever does.
func resolveCompletion(row db.ListTaskCompletionsForVelocityRow) (completion, bool) {
	hasDone := row.DoneOccurredAt.Valid
	hasClosed := row.ClosedAt.Valid
	switch {
	case !hasDone && !hasClosed:
		return completion{}, false
	case hasDone && (!hasClosed || !row.DoneOccurredAt.Time.After(row.ClosedAt.Time)):
		return completion{at: row.DoneOccurredAt.Time, actor: row.DoneActorKind}, true
	default:
		return completion{at: row.ClosedAt.Time, actor: ""}, true
	}
}

// Compute returns projectID's velocity: completed-task throughput bucketed
// by interval, over tasks whose resolved completion time falls in [from,
// to] (either bound may be nil, meaning unbounded). It returns ErrNotFound
// if projectID does not exist or the caller isn't a member, mirroring
// flowmetrics.Service.Compute.
func (s *Service) Compute(ctx context.Context, ownerID, projectID uuid.UUID, from, to *time.Time, interval metricsperiod.Interval) (Metrics, error) {
	err := s.projects.Authorize(ctx, ownerID, projectID, project.RoleViewer)
	switch {
	case err == nil:
	case errors.Is(err, project.ErrNotFound):
		return Metrics{}, ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return Metrics{}, ErrForbidden
	default:
		return Metrics{}, fmt.Errorf("velocity: authorize: %w", err)
	}

	rows, err := s.q.ListTaskCompletionsForVelocity(ctx, db.ListTaskCompletionsForVelocityParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return Metrics{}, fmt.Errorf("velocity: list task completions: %w", err)
	}

	openCount, err := s.q.CountOpenTasksForVelocity(ctx, db.CountOpenTasksForVelocityParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return Metrics{}, fmt.Errorf("velocity: count open tasks: %w", err)
	}

	var completions []completion
	for _, row := range rows {
		c, ok := resolveCompletion(row)
		if !ok {
			continue
		}
		if from != nil && c.at.Before(*from) {
			continue
		}
		if to != nil && c.at.After(*to) {
			continue
		}
		completions = append(completions, c)
	}

	type bucket struct {
		completed, byUser, byAgent, byUnknown int
	}
	buckets := make(map[time.Time]*bucket)
	observed := make([]time.Time, 0, len(completions))
	for _, c := range completions {
		observed = append(observed, c.at)
		key := metricsperiod.BucketStart(c.at, interval)
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.completed++
		switch c.actor {
		case "user":
			b.byUser++
		case "agent":
			b.byAgent++
		default:
			b.byUnknown++
		}
	}

	starts, truncated := metricsperiod.Timeline(interval, from, to, observed)
	now := time.Now()
	periods := make([]Period, len(starts))
	for i, start := range starts {
		b := buckets[start]
		if b == nil {
			b = &bucket{}
		}
		end := metricsperiod.BucketEnd(start, interval)
		periods[i] = Period{
			Start:              start,
			End:                end,
			Completed:          b.completed,
			CompletedByUser:    b.byUser,
			CompletedByAgent:   b.byAgent,
			CompletedByUnknown: b.byUnknown,
			Complete:           !end.After(now),
		}
	}
	// MovingAverage needs every period's Completed already filled in, so it
	// is a second pass over the now-complete slice rather than computed
	// inline in the loop above.
	for i := range periods {
		first := i - MovingAverageWindow + 1
		if first < 0 {
			first = 0
		}
		var sum int
		for j := first; j <= i; j++ {
			sum += periods[j].Completed
		}
		periods[i].MovingAverage = float64(sum) / float64(i-first+1)
	}

	var averageVelocity, forecastPeriods *float64
	var sum, count int
	for i := len(periods) - 1; i >= 0 && count < MovingAverageWindow; i-- {
		if !periods[i].Complete {
			continue
		}
		sum += periods[i].Completed
		count++
	}
	if count > 0 {
		v := float64(sum) / float64(count)
		averageVelocity = &v
		if v > 0 {
			f := float64(openCount) / v
			forecastPeriods = &f
		}
	}

	return Metrics{
		From:            from,
		To:              to,
		Interval:        interval,
		Truncated:       truncated,
		Periods:         periods,
		OpenTaskCount:   int(openCount),
		AverageVelocity: averageVelocity,
		ForecastPeriods: forecastPeriods,
	}, nil
}
