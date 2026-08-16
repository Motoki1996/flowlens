// Package flowmetrics computes per-task stage lead times (issue #171),
// built on top of the task_progress_events log #169 introduced and the
// progress convention #170 asks agents to follow. It measures the five
// stages a task moves through: waiting to start, AI-driven implementation,
// review/merge, completion processing, and cumulative time blocked. It is
// read-only and derives everything from task_progress_events and
// merge_requests already recorded/synced — there is nothing to write.
//
// Like internal/deliverymetrics, every stage is reported as a median and
// p90, never a mean, and only over tasks where both ends of that stage are
// known: a task that hasn't reached a stage's end yet is excluded from it
// rather than counted as a zero duration. A task with no linked merge
// request (no code change involved) is excluded from the implementation
// and review-and-merge stages by the same rule — that is expected, not a
// bug: those two stages measure code-review lead time, which doesn't exist
// for a task that never produced a merge request.
package flowmetrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service, matching deliverymetrics' own so
// callers can share error-mapping code.
var (
	ErrNotFound  = errors.New("flowmetrics: not found")
	ErrForbidden = errors.New("flowmetrics: forbidden")
)

// StageStats summarizes one stage's durations, in hours, across the tasks
// that reached both of the stage's endpoints in range. Count is 0 (and
// Median/P90 nil) when no task in range has both ends of the stage.
type StageStats struct {
	Count  int      `json:"count"`
	Median *float64 `json:"medianHours"`
	P90    *float64 `json:"p90Hours"`
}

// Metrics is the aggregation Service.Compute returns: FlowLens's five work
// stages (see the table in issue #171).
type Metrics struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`

	// WaitingToStart is tasks.created_at -> the first in_progress transition.
	WaitingToStart StageStats `json:"waitingToStart"`
	// Implementation is the first in_progress transition -> the linked
	// merge request's gitlab_created_at.
	Implementation StageStats `json:"implementation"`
	// ReviewAndMerge is the linked merge request's gitlab_created_at ->
	// merged_at.
	ReviewAndMerge StageStats `json:"reviewAndMerge"`
	// Completion is merged_at -> the first done transition.
	Completion StageStats `json:"completion"`
	// Blocked is each task's cumulative time spent in on_hold, summed
	// across every closed on_hold interval (a still-open one doesn't
	// count, the same "both ends known" rule every other stage follows).
	Blocked StageStats `json:"blocked"`
}

// Service is the flow-metrics domain service. Read-only, like
// deliverymetrics.Service: there is no write path here at all.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a flowmetrics Service. projects verifies project
// membership before any read.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

func toTimestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

// Compute returns projectID's flow metrics for tasks created in [from, to]
// (either bound may be nil, meaning unbounded). It returns ErrNotFound if
// projectID does not exist or the caller isn't a member, mirroring
// deliverymetrics.Service.Compute.
func (s *Service) Compute(ctx context.Context, ownerID, projectID uuid.UUID, from, to *time.Time) (Metrics, error) {
	err := s.projects.Authorize(ctx, ownerID, projectID, project.RoleViewer)
	switch {
	case err == nil:
	case errors.Is(err, project.ErrNotFound):
		return Metrics{}, ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return Metrics{}, ErrForbidden
	default:
		return Metrics{}, fmt.Errorf("flowmetrics: authorize: %w", err)
	}

	since, until := toTimestamptz(from), toTimestamptz(to)

	tasks, err := s.q.ListTasksForFlowMetrics(ctx, db.ListTasksForFlowMetricsParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
		Since:       since,
		Until:       until,
	})
	if err != nil {
		return Metrics{}, fmt.Errorf("flowmetrics: list tasks: %w", err)
	}

	events, err := s.q.ListTaskProgressEventsForFlowMetrics(ctx, db.ListTaskProgressEventsForFlowMetricsParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
		Since:       since,
		Until:       until,
	})
	if err != nil {
		return Metrics{}, fmt.Errorf("flowmetrics: list progress events: %w", err)
	}

	eventsByTask := make(map[uuid.UUID][]db.ListTaskProgressEventsForFlowMetricsRow)
	for _, e := range events {
		eventsByTask[e.TaskID] = append(eventsByTask[e.TaskID], e)
	}

	var (
		waitingToStart []float64
		implementation []float64
		reviewAndMerge []float64
		completion     []float64
		blocked        []float64
	)
	for _, t := range tasks {
		taskEvents := eventsByTask[t.ID]
		firstInProgress := firstOccurrence(taskEvents, "in_progress")
		firstDone := firstOccurrence(taskEvents, "done")

		if firstInProgress != nil && t.CreatedAt.Valid {
			waitingToStart = append(waitingToStart, firstInProgress.Sub(t.CreatedAt.Time).Hours())
		}
		if firstInProgress != nil && t.MrGitlabCreatedAt.Valid {
			implementation = append(implementation, t.MrGitlabCreatedAt.Time.Sub(*firstInProgress).Hours())
		}
		if t.MrGitlabCreatedAt.Valid && t.MrMergedAt.Valid {
			reviewAndMerge = append(reviewAndMerge, t.MrMergedAt.Time.Sub(t.MrGitlabCreatedAt.Time).Hours())
		}
		if t.MrMergedAt.Valid && firstDone != nil {
			completion = append(completion, firstDone.Sub(t.MrMergedAt.Time).Hours())
		}
		if hours, ok := blockedHours(taskEvents); ok {
			blocked = append(blocked, hours)
		}
	}

	return Metrics{
		From:           from,
		To:             to,
		WaitingToStart: stageStats(waitingToStart),
		Implementation: stageStats(implementation),
		ReviewAndMerge: stageStats(reviewAndMerge),
		Completion:     stageStats(completion),
		Blocked:        stageStats(blocked),
	}, nil
}

// firstOccurrence returns the occurred_at of the first event transitioning
// to toProgress, or nil if the task's timeline never reaches it. events
// must already be oldest-first (ListTaskProgressEventsForFlowMetrics
// guarantees this).
func firstOccurrence(events []db.ListTaskProgressEventsForFlowMetricsRow, toProgress string) *time.Time {
	for _, e := range events {
		if e.ToProgress == toProgress && e.OccurredAt.Valid {
			t := e.OccurredAt.Time
			return &t
		}
	}
	return nil
}

// blockedHours sums a task's closed on_hold intervals: every stretch from
// an event transitioning to "on_hold" to the next event on that task,
// whatever it transitions to next. A task still on_hold with no further
// event yet has an open interval, which isn't counted. ok is false when
// the task was never on_hold for a closed interval, so it is excluded from
// the stat rather than reported as a zero-hour block.
func blockedHours(events []db.ListTaskProgressEventsForFlowMetricsRow) (hours float64, ok bool) {
	var (
		total  float64
		since  time.Time
		inHold bool
	)
	for _, e := range events {
		if !e.OccurredAt.Valid {
			continue
		}
		if inHold {
			total += e.OccurredAt.Time.Sub(since).Hours()
			inHold = false
			ok = true
		}
		if e.ToProgress == "on_hold" {
			since = e.OccurredAt.Time
			inHold = true
		}
	}
	return total, ok
}

func stageStats(values []float64) StageStats {
	if len(values) == 0 {
		return StageStats{}
	}
	median, p90 := medianAndP90(values)
	return StageStats{Count: len(values), Median: &median, P90: &p90}
}

// medianAndP90 duplicates deliverymetrics' own nearest-rank implementation
// rather than sharing it (issue #171: two small, independent aggregations
// — not worth abstracting this early).
func medianAndP90(values []float64) (median, p90 float64) {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	return percentile(sorted, 0.5), percentile(sorted, 0.9)
}

// percentile returns the p-th percentile (0..1) of an already-sorted slice
// via nearest-rank: rank = ceil(p * n), 1-indexed, clamped to the slice.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	rank := int(math.Ceil(p * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
