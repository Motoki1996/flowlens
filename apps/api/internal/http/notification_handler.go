package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/notification"
)

type putNotificationSettingsRequest struct {
	WebhookURL string `json:"webhookUrl"`
	Enabled    bool   `json:"enabled"`
	SendHour   int    `json:"sendHour"`
}

// handlePutNotificationSettings saves (creating or replacing) the daily
// digest notification settings for one project. Owner-only, since the
// webhook_url is an outbound destination a lesser role should not be able
// to redirect (issue #109).
func (s *Server) handlePutNotificationSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	var req putNotificationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}

	settings, err := s.notifications.Save(r.Context(), u.ID, projectID, req.WebhookURL, req.Enabled, req.SendHour)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// handleGetNotificationSettings returns a project's notification settings,
// or their unconfigured defaults if never saved.
func (s *Server) handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	projectID, ok := projectIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}

	settings, err := s.notifications.Get(r.Context(), u.ID, projectID)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// writeNotificationError maps a notification domain error to its HTTP
// response.
func writeNotificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, notification.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case errors.Is(err, notification.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient project role")
	case errors.Is(err, notification.ErrInvalidWebhookURL):
		writeError(w, http.StatusBadRequest, "invalid_webhook_url", "webhook_url must be an absolute http(s) URL")
	case errors.Is(err, notification.ErrInvalidSendHour):
		writeError(w, http.StatusBadRequest, "invalid_send_hour", "send_hour must be between 0 and 23")
	default:
		slog.Error("notification settings request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
