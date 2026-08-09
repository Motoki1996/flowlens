package sync

import "github.com/prometheus/client_golang/prometheus"

// Exported for sync_test (an external test package): the metrics vars in
// metrics.go stay unexported production-side, but tests need a handle on
// them to assert the worker actually updates its own counters/gauges.

func JobsProcessedTotalForTest() *prometheus.CounterVec { return jobsProcessedTotal }

func PendingJobsGaugeForTest() prometheus.Gauge { return pendingJobsGauge }

func OldestPendingJobAgeSecondsForTest() prometheus.Gauge { return oldestPendingJobAgeSeconds }
