package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/progresssettings"
)

type putProgressSyncSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

// handlePutProgressSyncSettings saves (creating or replacing) whether a
// project's linked GitLab issue closing also moves its task's progress to
// 'done'. Owner-only, since it's an opt-in exception to progress otherwise
// never moving via the GitLab sync path (issue #202).
func (s *Server) handlePutProgressSyncSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req putProgressSyncSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	settings, err := s.progressSyncSettings.Save(r.Context(), u.ID, projectID, req.Enabled)
	if err != nil {
		writeProgressSyncSettingsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// handleGetProgressSyncSettings returns a project's progress-sync settings,
// or their unconfigured (disabled) default if never saved.
func (s *Server) handleGetProgressSyncSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	settings, err := s.progressSyncSettings.Get(r.Context(), u.ID, projectID)
	if err != nil {
		writeProgressSyncSettingsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// writeProgressSyncSettingsError maps a progresssettings domain error to its
// HTTP response.
func writeProgressSyncSettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, progresssettings.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case errors.Is(err, progresssettings.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	default:
		slog.Error("progress sync settings request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
