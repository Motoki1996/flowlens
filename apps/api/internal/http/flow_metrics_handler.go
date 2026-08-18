package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/flowmetrics"
)

// handleGetProjectFlowMetrics returns the project's stage-level lead-time
// metrics (issue #171): how long tasks spend waiting to start, in
// AI-driven implementation, in review/merge, and in completion processing,
// plus cumulative time blocked, over the optional ?from=/?to= YYYY-MM-DD
// range (unbounded when omitted, bounding tasks.created_at). Session-only,
// like handleGetProjectMetrics — a chart for a human reading the Project
// single view, not part of the AI-facing bearer-token allowlist.
//
// ?interval=week|month|year (issue #188) additionally buckets the same
// stats into a "periods" time series; omitted, it returns exactly what this
// endpoint returned before #188.
func (s *Server) handleGetProjectFlowMetrics(w http.ResponseWriter, r *http.Request) {
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

	metrics, err := s.flowMetrics.Compute(r.Context(), u.ID, projectID, from, to, interval)
	if err != nil {
		switch {
		case errors.Is(err, flowmetrics.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "project not found")
		case errors.Is(err, flowmetrics.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
		default:
			slog.Error("project flow metrics request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
