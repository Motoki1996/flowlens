package http

import (
	"net/http"
)

// handleHealth is a liveness/readiness probe. It reports the API status
// and whether the database is reachable.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"status": "ok"}
	status := http.StatusOK

	if err := s.pool.Ping(r.Context()); err != nil {
		resp["status"] = "degraded"
		resp["database"] = "unreachable"
		status = http.StatusServiceUnavailable
	} else {
		resp["database"] = "ok"
	}
	writeJSON(w, status, resp)
}
