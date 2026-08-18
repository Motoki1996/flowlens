// Package deliverymetrics computes the delivery-flow metrics ADR-0011
// designed and #111/#112 populated (issue #113): open->first-review and
// first-review->merge durations, MR size distribution, pipeline success
// rate and merged-MR throughput. It is read-only and derives everything
// from merge_requests already synced by internal/mrsync — there is nothing
// to write.
//
// Every distribution is reported as median and p90, never a mean: lead
// time is reliably skewed by a handful of very slow reviews, and a mean
// would let a few outliers hide behind a falsely comfortable "average".
package deliverymetrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/metricsperiod"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service, matching the mergerequest package's
// own so callers can share error-mapping code.
var (
	ErrNotFound  = errors.New("deliverymetrics: not found")
	ErrForbidden = errors.New("deliverymetrics: forbidden")
)

// DurationStats summarizes a set of durations, in hours. Count is 0 (and
// Median/P90 nil) when no merge request in range has both timestamps a
// stat needs — e.g. OpenToFirstReview excludes a merge request nobody has
// reviewed yet, rather than treating "not yet reviewed" as a zero duration.
type DurationStats struct {
	Count  int      `json:"count"`
	Median *float64 `json:"medianHours"`
	P90    *float64 `json:"p90Hours"`
}

// SizeStats summarizes a set of merge-request size measures (additions,
// deletions, changed files). Same shape as DurationStats, without the
// "hours" unit.
type SizeStats struct {
	Count  int      `json:"count"`
	Median *float64 `json:"median"`
	P90    *float64 `json:"p90"`
}

// PeriodStats is the set of stats reported both for the request's whole
// range (Metrics itself) and for each bucket within it (Period), when
// ?interval= is set — one shared field list so the two shapes can't drift.
type PeriodStats struct {
	OpenToFirstReview  DurationStats `json:"openToFirstReview"`
	FirstReviewToMerge DurationStats `json:"firstReviewToMerge"`

	Additions    SizeStats `json:"additions"`
	Deletions    SizeStats `json:"deletions"`
	ChangedFiles SizeStats `json:"changedFiles"`

	// PipelineSuccessRate is successful / (successful + failed) pipelines,
	// 0..1, or nil when no merge request in range has a decided pipeline
	// (still running/pending/skipped/canceled/manual/no pipeline at all
	// don't count toward either side — they haven't produced a verdict).
	PipelineSuccessRate *float64 `json:"pipelineSuccessRate"`

	// Throughput is the count of merge requests with state "merged" among
	// those in range.
	Throughput int `json:"throughput"`
}

// Period is one interval-sized bucket of PeriodStats (issue #188), assigned
// by merge_requests.gitlab_created_at — the same "created" cohort basis
// Metrics itself bounds by via from/to.
type Period struct {
	// Start and End bound the bucket in UTC; End is exclusive.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	PeriodStats
}

// Metrics is the aggregation Service.Compute returns.
type Metrics struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`

	// Interval is the ?interval= the request asked to bucket Periods by, or
	// nil when omitted — in which case Periods is empty and Truncated is
	// false, and every other field is exactly what this endpoint returned
	// before issue #188 added bucketing.
	Interval  *metricsperiod.Interval `json:"interval"`
	Truncated bool                    `json:"truncated"`
	Periods   []Period                `json:"periods"`

	PeriodStats
}

// Service is the delivery-metrics domain service. Read-only, like
// mergerequest.Service: there is no write path here at all.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a deliverymetrics Service. projects verifies
// project membership before any read.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

func toTimestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

// Compute returns projectID's delivery metrics for merge requests GitLab
// created in [from, to] (either bound may be nil, meaning unbounded). It
// returns ErrNotFound if projectID does not exist or the caller isn't a
// member, mirroring mergerequest.Service.List.
//
// When interval is non-nil, the response also carries Periods: the same
// stats broken into interval-sized buckets of gitlab_created_at, gap-filled
// across the covered range and capped at metricsperiod.MaxPeriods (newest
// kept, Truncated reported). interval nil leaves Periods empty and every
// other field exactly as it was before issue #188.
func (s *Service) Compute(ctx context.Context, ownerID, projectID uuid.UUID, from, to *time.Time, interval *metricsperiod.Interval) (Metrics, error) {
	err := s.projects.Authorize(ctx, ownerID, projectID, project.RoleViewer)
	switch {
	case err == nil:
	case errors.Is(err, project.ErrNotFound):
		return Metrics{}, ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return Metrics{}, ErrForbidden
	default:
		return Metrics{}, fmt.Errorf("deliverymetrics: authorize: %w", err)
	}

	rows, err := s.q.ListMergeRequestsForMetrics(ctx, db.ListMergeRequestsForMetricsParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
		Since:       toTimestamptz(from),
		Until:       toTimestamptz(to),
	})
	if err != nil {
		return Metrics{}, fmt.Errorf("deliverymetrics: list: %w", err)
	}

	var (
		openToFirstReview  []float64
		firstReviewToMerge []float64
		additions          []float64
		deletions          []float64
		changedFiles       []float64
		pipelineSuccess    int
		pipelineFailed     int
		throughput         int

		buckets  = make(map[time.Time]*bucketAcc)
		observed []time.Time
	)
	for _, row := range rows {
		var bucket *bucketAcc
		if interval != nil && row.GitlabCreatedAt.Valid {
			observed = append(observed, row.GitlabCreatedAt.Time)
			key := metricsperiod.BucketStart(row.GitlabCreatedAt.Time, *interval)
			bucket = buckets[key]
			if bucket == nil {
				bucket = &bucketAcc{}
				buckets[key] = bucket
			}
		}

		additions = append(additions, float64(row.Additions))
		deletions = append(deletions, float64(row.Deletions))
		changedFiles = append(changedFiles, float64(row.ChangedFiles))
		if bucket != nil {
			bucket.additions = append(bucket.additions, float64(row.Additions))
			bucket.deletions = append(bucket.deletions, float64(row.Deletions))
			bucket.changedFiles = append(bucket.changedFiles, float64(row.ChangedFiles))
		}

		if row.State == "merged" {
			throughput++
			if bucket != nil {
				bucket.throughput++
			}
		}

		switch row.PipelineStatus {
		case "success":
			pipelineSuccess++
			if bucket != nil {
				bucket.pipelineSuccess++
			}
		case "failed":
			pipelineFailed++
			if bucket != nil {
				bucket.pipelineFailed++
			}
		}

		if row.GitlabCreatedAt.Valid && row.FirstReviewedAt.Valid {
			v := row.FirstReviewedAt.Time.Sub(row.GitlabCreatedAt.Time).Hours()
			openToFirstReview = append(openToFirstReview, v)
			if bucket != nil {
				bucket.openToFirstReview = append(bucket.openToFirstReview, v)
			}
		}
		if row.FirstReviewedAt.Valid && row.MergedAt.Valid {
			v := row.MergedAt.Time.Sub(row.FirstReviewedAt.Time).Hours()
			firstReviewToMerge = append(firstReviewToMerge, v)
			if bucket != nil {
				bucket.firstReviewToMerge = append(bucket.firstReviewToMerge, v)
			}
		}
	}

	metrics := Metrics{
		From: from,
		To:   to,
		PeriodStats: PeriodStats{
			OpenToFirstReview:   durationStats(openToFirstReview),
			FirstReviewToMerge:  durationStats(firstReviewToMerge),
			Additions:           sizeStats(additions),
			Deletions:           sizeStats(deletions),
			ChangedFiles:        sizeStats(changedFiles),
			PipelineSuccessRate: pipelineSuccessRate(pipelineSuccess, pipelineFailed),
			Throughput:          throughput,
		},
	}

	if interval != nil {
		starts, truncated := metricsperiod.Timeline(*interval, from, to, observed)
		periods := make([]Period, len(starts))
		for i, start := range starts {
			b := buckets[start]
			if b == nil {
				b = &bucketAcc{}
			}
			periods[i] = Period{
				Start: start,
				End:   metricsperiod.BucketEnd(start, *interval),
				PeriodStats: PeriodStats{
					OpenToFirstReview:   durationStats(b.openToFirstReview),
					FirstReviewToMerge:  durationStats(b.firstReviewToMerge),
					Additions:           sizeStats(b.additions),
					Deletions:           sizeStats(b.deletions),
					ChangedFiles:        sizeStats(b.changedFiles),
					PipelineSuccessRate: pipelineSuccessRate(b.pipelineSuccess, b.pipelineFailed),
					Throughput:          b.throughput,
				},
			}
		}
		metrics.Interval = interval
		metrics.Truncated = truncated
		metrics.Periods = periods
	}

	return metrics, nil
}

// bucketAcc accumulates one interval bucket's worth of raw samples/counts,
// mirroring the flat variables Compute also keeps for the unbucketed total.
type bucketAcc struct {
	openToFirstReview, firstReviewToMerge []float64
	additions, deletions, changedFiles    []float64
	pipelineSuccess, pipelineFailed       int
	throughput                            int
}

func pipelineSuccessRate(success, failed int) *float64 {
	decided := success + failed
	if decided == 0 {
		return nil
	}
	rate := float64(success) / float64(decided)
	return &rate
}

func durationStats(values []float64) DurationStats {
	if len(values) == 0 {
		return DurationStats{}
	}
	median, p90 := medianAndP90(values)
	return DurationStats{Count: len(values), Median: &median, P90: &p90}
}

func sizeStats(values []float64) SizeStats {
	if len(values) == 0 {
		return SizeStats{}
	}
	median, p90 := medianAndP90(values)
	return SizeStats{Count: len(values), Median: &median, P90: &p90}
}

// medianAndP90 returns the 50th and 90th percentile of values, using the
// nearest-rank method (no interpolation): simple, deterministic, and exact
// for the single-value/small-n cases this feature's data will mostly be.
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
