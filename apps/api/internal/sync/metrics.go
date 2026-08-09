package sync

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics for issue #96: worker throughput/latency by job kind, plus two
// queue-depth gauges — pending job count and the oldest pending job's age —
// which are what actually surface a stuck worker, as opposed to the
// counters below which only show it is (or isn't) making progress.
var (
	jobsProcessedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "flowlens_sync_jobs_processed_total",
		Help: "Total sync jobs the worker has finished executing, labeled by job kind and outcome.",
	}, []string{"kind", "outcome"})

	jobDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "flowlens_sync_job_duration_seconds",
		Help:    "Time spent executing a sync job's handler, labeled by job kind.",
		Buckets: prometheus.DefBuckets,
	}, []string{"kind"})

	pendingJobsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "flowlens_sync_pending_jobs",
		Help: "Number of sync_jobs rows currently pending.",
	})

	oldestPendingJobAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "flowlens_sync_oldest_pending_job_age_seconds",
		Help: "Age in seconds of the oldest pending sync job, or 0 when the queue is empty.",
	})
)

// outcome labels for jobsProcessedTotal.
const (
	outcomeSucceeded = "succeeded"
	outcomeRetry     = "retry"
	outcomeFailed    = "failed"
)
