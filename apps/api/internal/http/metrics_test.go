package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// looksLikeUUID reports whether s parses as a UUID, so the cardinality test
// below can tell "a route pattern that happens to contain a hyphen (e.g.
// gitlab-connection)" apart from "a raw UUID leaked into the label".
func looksLikeUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// TestMetricsMiddleware_RouteLabelIsChiPattern verifies the completion
// condition from issue #96: the "route" label on flowlens_http_requests_total
// is chi's route pattern (e.g. "/api/v1/projects/{projectID}"), never the
// raw request path, so hitting the same route for many different resource
// IDs never grows the metric's cardinality.
func TestMetricsMiddleware_RouteLabelIsChiPattern(t *testing.T) {
	s, _ := newTestServer(t)

	// Two different, unrelated project IDs hitting the same route shape.
	for _, id := range []uuid.UUID{uuid.New(), uuid.New()} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+id.String(), nil)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
	}

	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	var routeValues []string
	for _, mf := range mfs {
		if mf.GetName() != "flowlens_http_requests_total" {
			continue
		}
		for _, m := range mf.Metric {
			for _, lp := range m.Label {
				if lp.GetName() == "route" {
					routeValues = append(routeValues, lp.GetValue())
				}
			}
		}
	}

	require.NotEmpty(t, routeValues)
	for _, route := range routeValues {
		for _, segment := range strings.Split(route, "/") {
			assert.False(t, looksLikeUUID(segment), "route label %q contains a raw UUID path segment instead of a chi pattern", route)
		}
	}
	assert.Contains(t, routeValues, "/api/v1/projects/{projectID}")
}

// TestHandleMetrics_ServesPrometheusFormat exercises GET /metrics end to
// end: it must be reachable with no auth and return the counter this same
// request itself just incremented.
func TestHandleMetrics_ServesPrometheusFormat(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	s.Router().ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "flowlens_http_requests_total"), "metrics body should contain the HTTP request counter")
}
