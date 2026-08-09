package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/syncjob"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// jobIDFromURL parses the {jobID} path parameter. A malformed ID is reported
// as "not found" so it is indistinguishable from an unknown one, the same
// convention as linkIDFromURL/projectIDFromURL.
func jobIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "jobID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListFailedSyncJobs returns a project's permanently-failed sync jobs
// (issue #97), newest first, scoped to the authenticated user. Session-only
// — not on the bearer-token route allowlist.
func (s *Server) handleListFailedSyncJobs(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	jobs, err := s.syncJobs.ListFailed(r.Context(), u.ID, projectID)
	if err != nil {
		writeSyncJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleRetrySyncJob puts a 'failed' sync job back to 'pending' so
// internal/sync's Worker picks it up again, scoped to the authenticated user
// through the job's project. Session-only, mirroring
// handleRetryWebhookEvent.
func (s *Server) handleRetrySyncJob(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	jobID, ok := jobIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "sync job not found")
		return
	}

	job, err := s.syncJobs.Retry(r.Context(), u.ID, jobID)
	if err != nil {
		writeSyncJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// writeSyncJobError maps a syncjob domain error to its HTTP response.
func writeSyncJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, syncjob.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "sync job not found")
	case errors.Is(err, syncjob.ErrNotFailed):
		writeError(w, http.StatusConflict, "job_not_failed", "only a failed sync job can be retried")
	default:
		slog.Error("sync job request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
