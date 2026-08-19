package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/webhookevent"
)

// webhookMaxBodyBytes bounds an inbound GitLab webhook delivery's body
// (docs/plans/issue-sync.md, Security: "bounded in body size").
const webhookMaxBodyBytes = 1 << 20 // 1MB

// webhookRateLimit and webhookRateLimitWindow bound how many deliveries a
// single client IP + link can send in a window (docs/plans/issue-sync.md,
// Security: "rate-limited"). GitLab redelivers on failure/timeout, and a
// misconfigured or malicious sender should not be able to flood the
// database, but a legitimate burst of issue activity must still get
// through.
const (
	webhookRateLimit       = 60
	webhookRateLimitWindow = time.Minute
)

// gitlabIssueHookPayload extracts only the fields the receiver needs to
// store alongside the raw payload. The full payload is kept verbatim in
// webhook_events.payload for the apply pipeline to parse. It covers both
// "Issue Hook" (object_attributes.iid is the issue's own IID) and, since
// #104, "Note Hook" (object_attributes.iid is the note's own id — the
// issue it's attached to is the separate top-level "issue" object) payloads.
type gitlabIssueHookPayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID       int64  `json:"iid"`
		UpdatedAt string `json:"updated_at"`
	} `json:"object_attributes"`
	Issue struct {
		IID int64 `json:"iid"`
	} `json:"issue"`
}

// issueIID returns the GitLab issue IID a payload is about, regardless of
// whether it's an issue event (carried on object_attributes) or a note event
// (carried on the separate "issue" object).
func (p gitlabIssueHookPayload) issueIID() int64 {
	if p.ObjectKind == "note" {
		return p.Issue.IID
	}
	return p.ObjectAttributes.IID
}

// handleGitlabWebhook receives a GitLab project webhook delivery
// (docs/plans/issue-sync.md, "Inbound"). It is unauthenticated by session —
// linkID plus a constant-time X-Gitlab-Token comparison against that link's
// own secret is the authorization boundary (ADR-0008) — and it never
// applies the event: it only records it and returns fast, leaving
// application to the outbox worker (a later phase).
func (s *Server) handleGitlabWebhook(w http.ResponseWriter, r *http.Request) {
	linkID, ok := linkIDFromURL(r)
	if !ok {
		webhookEventsReceivedTotal.WithLabelValues(webhookMetricFailed).Inc()
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	if !s.webhookLimiter.Allow(s.clientIP(r) + ":" + linkID.String()) {
		webhookEventsReceivedTotal.WithLabelValues(webhookMetricFailed).Inc()
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	if err := s.webhookEvents.VerifyToken(r.Context(), linkID, r.Header.Get("X-Gitlab-Token")); err != nil {
		webhookEventsReceivedTotal.WithLabelValues(webhookMetricFailed).Inc()
		if errors.Is(err, webhookevent.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		slog.Error("verify webhook token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if r.ContentLength > webhookMaxBodyBytes {
		webhookEventsReceivedTotal.WithLabelValues(webhookMetricFailed).Inc()
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookMaxBodyBytes))
	if err != nil {
		webhookEventsReceivedTotal.WithLabelValues(webhookMetricFailed).Inc()
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
		return
	}

	var payload gitlabIssueHookPayload
	_ = json.Unmarshal(body, &payload) // best-effort: an unparsable payload is still recorded as-is

	eventName := r.Header.Get("X-Gitlab-Event")
	status, skipReason, respStatus := webhookevent.StatusPending, "", http.StatusOK
	if !webhookevent.IsSupportedEventHeader(eventName) {
		status, skipReason, respStatus = webhookevent.StatusSkipped, webhookevent.SkipReasonUnsupportedEvent, http.StatusAccepted
	}

	updatedAt, ok := gitlab.ParseHookTime(payload.ObjectAttributes.UpdatedAt)
	if !ok && payload.ObjectAttributes.UpdatedAt != "" {
		slog.Warn("unparsable gitlab webhook updated_at", "raw_updated_at", payload.ObjectAttributes.UpdatedAt)
	}

	deliveryUUID := r.Header.Get("X-Gitlab-Event-UUID")
	if deliveryUUID == "" {
		// GitLab added this header in 14.8; on-prem GitLab CE older than that
		// never sends it. Leaving it empty would collide on the
		// UNIQUE(linked_gitlab_project_id, delivery_uuid) constraint — unlike
		// SQL NULL, '' equals '' — so every delivery after the first would be
		// silently dropped as a duplicate. Minting one here disables
		// dedup for these deliveries, but that's unavoidable without the
		// header; double-apply is still caught by the later echo/stale guard
		// in internal/webhookapply.
		deliveryUUID = uuid.NewString()
	}

	err = s.webhookEvents.Record(r.Context(), webhookevent.RecordParams{
		LinkedGitlabProjectID: linkID,
		DeliveryUUID:          deliveryUUID,
		EventName:             eventName,
		ObjectKind:            payload.ObjectKind,
		GitlabIssueIID:        payload.issueIID(),
		Payload:               body,
		GitlabUpdatedAt:       updatedAt,
		Status:                status,
		SkipReason:            skipReason,
	})
	if err != nil {
		webhookEventsReceivedTotal.WithLabelValues(webhookMetricFailed).Inc()
		slog.Error("record webhook event", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	metricStatus := webhookMetricProcessed
	if status == webhookevent.StatusSkipped {
		metricStatus = webhookMetricSkipped
	}
	webhookEventsReceivedTotal.WithLabelValues(metricStatus).Inc()

	w.WriteHeader(respStatus)
}
