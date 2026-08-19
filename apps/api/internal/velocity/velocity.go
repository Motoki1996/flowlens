// Package velocity computes throughput — completed tasks per period (issue
// #195) — as distinct from internal/deliverymetrics (merge-request lead
// time) and internal/flowmetrics (per-task stage lead time): both of those
// measure how long one item took, this measures how many items finished in
// a window. Velocity is reported two ways at once: a raw completed-task
// count, and a size-weighted point total (tasks.size, migration 000025).
// Both are additionally split by task_progress_events.actor_kind into
// user/agent/unknown — "how much throughput did the agent actually produce"
// is a number no other tool here can give.
//
// The count and the points answer different questions and neither is
// redundant: a count alone can be inflated for free by splitting tasks
// smaller, while points alone hide whether the work is arriving as a few
// large items or many small ones. Note issue #195 originally shipped this
// package count-only, on the grounds that story points rot when a human has
// to re-enter them every task; tasks.size is the narrower answer it left
// open — a five-value T-shirt scale, weighted here rather than typed in as
// a number.
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
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
)

// Sentinel errors returned by Service, matching flowmetrics/deliverymetrics'
// own so callers can share error-mapping code.
var (
	ErrNotFound  = errors.New("velocity: not found")
	ErrForbidden = errors.New("velocity: forbidden")
)

// sizePoints is the size -> weight table velocity multiplies by. It is
// deliberately the *only* place these weights exist: internal/database/queries/
// velocity.sql groups by size and leaves the arithmetic here rather than
// summing a CASE in SQL, so the two can never drift apart.
//
// The steps are Fibonacci-ish rather than linear (1,2,3,5,8 not 1,2,3,4,5)
// because uncertainty grows faster than size does — an XL task is far more
// than 5/3 of an M in practice. A size outside the table (impossible while
// the 000025 CHECK constraint holds) weighs 0 rather than panicking, since
// a metrics endpoint should degrade rather than fail.
var sizePoints = map[string]int{
	task.SizeXS: 1,
	task.SizeS:  2,
	task.SizeM:  3,
	task.SizeL:  5,
	task.SizeXL: 8,
}

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

	// CompletedPoints is Completed weighted by each task's size (sizePoints),
	// and the ByUser/ByAgent/ByUnknown split follows exactly the same actor
	// attribution rule as the counts above, so the three always sum to it.
	CompletedPoints          int `json:"completedPoints"`
	CompletedPointsByUser    int `json:"completedPointsByUser"`
	CompletedPointsByAgent   int `json:"completedPointsByAgent"`
	CompletedPointsByUnknown int `json:"completedPointsByUnknown"`

	// MovingAverage is the simple average of Completed over this period and
	// up to MovingAverageWindow-1 preceding periods (fewer if not that many
	// exist yet). MovingAveragePoints is the same window over
	// CompletedPoints.
	MovingAverage       float64 `json:"movingAverage"`
	MovingAveragePoints float64 `json:"movingAveragePoints"`
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

	// OpenTaskPoints is OpenTaskCount weighted by size, and
	// AverageVelocityPoints/ForecastPeriodsByPoints are the point-denominated
	// counterparts of the two fields above, computed by the identical rules
	// (in particular AverageVelocityPoints also excludes still-running
	// periods). The points forecast is the more trustworthy of the two once
	// sizes are actually being set, since it accounts for the remaining work
	// being unusually large or small rather than assuming an average task.
	OpenTaskPoints          int      `json:"openTaskPoints"`
	AverageVelocityPoints   *float64 `json:"averageVelocityPoints"`
	ForecastPeriodsByPoints *float64 `json:"forecastPeriodsByPoints"`

	// SizedTaskRatio is the fraction of the completed tasks counted here
	// whose size is something other than the default 'm', 0..1 (0 when
	// nothing completed in range at all). Every task predating migration
	// 000025 reads as 'm', so until sizes are actually set the point series
	// is just 3x the count series — a caller showing points is expected to
	// use this to say so rather than presenting a scaled copy as new
	// information.
	SizedTaskRatio float64 `json:"sizedTaskRatio"`
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
	size  string // one of task.Size*, weighted through sizePoints
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
		return completion{at: row.DoneOccurredAt.Time, actor: row.DoneActorKind, size: row.Size}, true
	default:
		return completion{at: row.ClosedAt.Time, actor: "", size: row.Size}, true
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

	openRows, err := s.q.CountOpenTasksBySizeForVelocity(ctx, db.CountOpenTasksBySizeForVelocityParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return Metrics{}, fmt.Errorf("velocity: count open tasks: %w", err)
	}
	var openCount, openPoints int
	for _, row := range openRows {
		openCount += int(row.Count)
		openPoints += int(row.Count) * sizePoints[row.Size]
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
		completed, byUser, byAgent, byUnknown                int
		points, pointsByUser, pointsByAgent, pointsByUnknown int
	}
	buckets := make(map[time.Time]*bucket)
	observed := make([]time.Time, 0, len(completions))
	var sized int
	for _, c := range completions {
		observed = append(observed, c.at)
		if c.size != task.SizeM {
			sized++
		}
		points := sizePoints[c.size]
		key := metricsperiod.BucketStart(c.at, interval)
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.completed++
		b.points += points
		switch c.actor {
		case "user":
			b.byUser++
			b.pointsByUser += points
		case "agent":
			b.byAgent++
			b.pointsByAgent += points
		default:
			b.byUnknown++
			b.pointsByUnknown += points
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
			Start:                    start,
			End:                      end,
			Completed:                b.completed,
			CompletedByUser:          b.byUser,
			CompletedByAgent:         b.byAgent,
			CompletedByUnknown:       b.byUnknown,
			CompletedPoints:          b.points,
			CompletedPointsByUser:    b.pointsByUser,
			CompletedPointsByAgent:   b.pointsByAgent,
			CompletedPointsByUnknown: b.pointsByUnknown,
			Complete:                 !end.After(now),
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
		var sum, pointSum int
		for j := first; j <= i; j++ {
			sum += periods[j].Completed
			pointSum += periods[j].CompletedPoints
		}
		periods[i].MovingAverage = float64(sum) / float64(i-first+1)
		periods[i].MovingAveragePoints = float64(pointSum) / float64(i-first+1)
	}

	// Both averages walk the *complete* periods only, newest first. Skipping
	// a still-running period rather than stopping at it is the whole point:
	// an in-progress bucket is partial by construction and would drag the
	// average — and so the forecast — down every time the endpoint is called
	// mid-period.
	var averageVelocity, forecastPeriods *float64
	var averageVelocityPoints, forecastPeriodsByPoints *float64
	var sum, pointSum, count int
	for i := len(periods) - 1; i >= 0 && count < MovingAverageWindow; i-- {
		if !periods[i].Complete {
			continue
		}
		sum += periods[i].Completed
		pointSum += periods[i].CompletedPoints
		count++
	}
	if count > 0 {
		v := float64(sum) / float64(count)
		averageVelocity = &v
		if v > 0 {
			f := float64(openCount) / v
			forecastPeriods = &f
		}
		pv := float64(pointSum) / float64(count)
		averageVelocityPoints = &pv
		if pv > 0 {
			pf := float64(openPoints) / pv
			forecastPeriodsByPoints = &pf
		}
	}

	var sizedRatio float64
	if len(completions) > 0 {
		sizedRatio = float64(sized) / float64(len(completions))
	}

	return Metrics{
		From:                    from,
		To:                      to,
		Interval:                interval,
		Truncated:               truncated,
		Periods:                 periods,
		OpenTaskCount:           openCount,
		AverageVelocity:         averageVelocity,
		ForecastPeriods:         forecastPeriods,
		OpenTaskPoints:          openPoints,
		AverageVelocityPoints:   averageVelocityPoints,
		ForecastPeriodsByPoints: forecastPeriodsByPoints,
		SizedTaskRatio:          sizedRatio,
	}, nil
}
