package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/projectsync"
)

type createSyncRunRequest struct {
	// Full selects a full re-fetch of every issue over a diff against the
	// link's last successful sync (docs/plans/issue-sync.md's "let the user
	// choose between an incremental fetch and a full re-fetch", issue #25).
	Full bool `json:"full"`
}

// handleCreateSyncRun starts a manual re-sync (project.resync) for a linked
// GitLab project, scoped to the authenticated user. It returns 409 if a
// sync run is already in progress for this link.
func (s *Server) handleCreateSyncRun(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	var req createSyncRunRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
			return
		}
	}

	run, err := s.projectSync.TriggerResync(r.Context(), u.ID, linkID, req.Full)
	if err != nil {
		writeSyncRunError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// handleListSyncRuns returns a linked GitLab project's sync run history,
// newest first, scoped to the authenticated user.
func (s *Server) handleListSyncRuns(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	runs, err := s.projectSync.ListRuns(r.Context(), u.ID, linkID)
	if err != nil {
		writeSyncRunError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// writeSyncRunError maps a projectsync domain error to its HTTP response.
func writeSyncRunError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projectsync.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
	case errors.Is(err, projectsync.ErrRunInProgress):
		writeError(w, http.StatusConflict, "sync_run_in_progress", "a sync run is already in progress for this linked project")
	default:
		slog.Error("sync run request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
