package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTP request metrics (issue #96), labeled by chi's *route pattern* (e.g.
// "/projects/{projectID}") rather than the raw URL path, so an ID in the URL
// never becomes a label value — cardinality stays bounded to the size of
// the route table no matter how many projects/tasks/etc. exist.
var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "flowlens_http_requests_total",
		Help: "Total HTTP requests, labeled by method, route pattern and status.",
	}, []string{"method", "route", "status"})

	httpRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "flowlens_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, labeled by method, route pattern and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// webhookEventsReceivedTotal counts every inbound GitLab webhook delivery
	// by receive-time outcome: "processed" (recorded and ready for the apply
	// pipeline), "skipped" (recorded but an unsupported event type), or
	// "failed" (rejected before/instead of being recorded — bad token, rate
	// limit, oversized body, or a write error). It has no linkID/project
	// label, so cardinality is fixed at 3 regardless of how many GitLab
	// projects are linked.
	webhookEventsReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "flowlens_webhook_events_received_total",
		Help: "Total inbound GitLab webhook deliveries, labeled by receive-time status.",
	}, []string{"status"})
)

const (
	webhookMetricProcessed = "processed"
	webhookMetricSkipped   = "skipped"
	webhookMetricFailed    = "failed"
)

// metricsMiddleware records httpRequestsTotal/httpRequestDurationSeconds for
// every request. It must be mounted so it wraps route matching (i.e. via
// r.Use at the router's root, as requestLogger already is) so that by the
// time it reads the route pattern back out of the request context, chi has
// finished matching and populated it.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(sw.status)
		httpRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		httpRequestDurationSeconds.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
	})
}

// metricsHandler serves Prometheus text-format metrics, mounted
// unauthenticated at /metrics next to /healthz.
var metricsHandler = promhttp.Handler()
