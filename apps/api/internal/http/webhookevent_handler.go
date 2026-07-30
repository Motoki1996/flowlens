package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flowlens/api/internal/webhookevent"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// eventIDFromURL parses the {eventID} path parameter. A malformed ID is
// reported as "not found" so it is indistinguishable from an unknown one,
// the same convention as linkIDFromURL.
func eventIDFromURL(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "eventID"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// handleListWebhookEvents returns a page of a linked GitLab project's
// recorded webhook events, newest first, scoped to the authenticated user.
// Every event's payload is omitted; see handleGetWebhookEvent.
func (s *Server) handleListWebhookEvents(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}

	q := r.URL.Query()
	page, err := s.webhookEvents.List(r.Context(), u.ID, linkID, webhookevent.ListParams{
		Status:  q.Get("status"),
		Page:    atoiOrZero(q.Get("page")),
		PerPage: atoiOrZero(q.Get("per_page")),
	})
	if err != nil {
		writeWebhookEventError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Events   []webhookevent.Event `json:"events"`
		NextPage int                  `json:"nextPage"`
	}{Events: page.Events, NextPage: page.NextPage})
}

// handleGetWebhookEvent returns one recorded webhook event's full detail,
// including its raw payload, scoped to the authenticated user.
func (s *Server) handleGetWebhookEvent(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}
	eventID, ok := eventIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "webhook event not found")
		return
	}

	event, err := s.webhookEvents.Get(r.Context(), u.ID, linkID, eventID)
	if err != nil {
		writeWebhookEventError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

// handleRetryWebhookEvent puts a 'failed' event back to 'pending' so the
// apply worker (internal/webhookapply) picks it up again, scoped to the
// authenticated user.
func (s *Server) handleRetryWebhookEvent(w http.ResponseWriter, r *http.Request) {
	u, _ := userFromContext(r.Context())
	linkID, ok := linkIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "linked gitlab project not found")
		return
	}
	eventID, ok := eventIDFromURL(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "webhook event not found")
		return
	}

	event, err := s.webhookEvents.Retry(r.Context(), u.ID, linkID, eventID)
	if err != nil {
		writeWebhookEventError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

// writeWebhookEventError maps a webhookevent domain error to its HTTP
// response.
func writeWebhookEventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhookevent.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "webhook event not found")
	case errors.Is(err, webhookevent.ErrNotFailed):
		writeError(w, http.StatusConflict, "event_not_failed", "only a failed webhook event can be retried")
	default:
		slog.Error("webhook event request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
