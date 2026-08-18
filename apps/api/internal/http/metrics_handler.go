package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/deliverymetrics"
)

// handleGetProjectMetrics returns the project's delivery-flow metrics
// (issue #113): review/merge lead time, MR size distribution, pipeline
// success rate and throughput, over the optional ?from=/?to= YYYY-MM-DD
// range (unbounded when omitted). Session-only, like the rest of the
// /projects/{projectID} group this is mounted in — a chart for a human
// reading the Project single view, not part of the AI-facing bearer-token
// allowlist.
//
// ?interval=week|month|year (issue #188) additionally buckets the same
// stats into a "periods" time series; omitted, it returns exactly what this
// endpoint returned before #188.
func (s *Server) handleGetProjectMetrics(w http.ResponseWriter, r *http.Request) {
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

	metrics, err := s.deliveryMetrics.Compute(r.Context(), u.ID, projectID, from, to, interval)
	if err != nil {
		switch {
		case errors.Is(err, deliverymetrics.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "project not found")
		case errors.Is(err, deliverymetrics.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
		default:
			slog.Error("project metrics request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}
