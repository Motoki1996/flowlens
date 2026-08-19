package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/metricsperiod"
	"github.com/flowlens/api/internal/velocity"
)

// handleGetProjectVelocity returns the project's completed-task throughput
// per period (issue #195), split by user/agent/unknown actor, over the
// optional ?from=/?to= YYYY-MM-DD range (unbounded when omitted, bounding
// each task's resolved *completion* time — not tasks.created_at like
// flow-metrics/metrics). Session-only, like the other two metrics
// endpoints — a chart for a human reading the Project single view, not
// part of the AI-facing bearer-token allowlist.
//
// Unlike ?interval= on /metrics and /flow-metrics, which defaults to
// "don't bucket" when omitted, velocity's periods *are* the metric — an
// omitted ?interval= here defaults to "week" rather than nil.
func (s *Server) handleGetProjectVelocity(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	from, err := parseDateQueryParam(r, "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	to, err := parseDateQueryParam(r, "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	interval, err := parseIntervalQueryParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if interval == nil {
		week := metricsperiod.Week
		interval = &week
	}

	metrics, err := s.velocity.Compute(r.Context(), u.ID, projectID, from, to, *interval)
	if err != nil {
		switch {
		case errors.Is(err, velocity.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "project not found")
		case errors.Is(err, velocity.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
		default:
			slog.Error("project velocity request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
