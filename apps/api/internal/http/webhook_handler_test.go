package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testWebhookSecret = "webhook-secret-value"
	issueHookPayload  = `{"object_kind":"issue","object_attributes":{"iid":1,"updated_at":"2023-01-01T12:00:00Z"}}`
)

// seedWebhookLink seeds a linked GitLab project whose webhook is already
// "registered" with secret, returning its ID (the {linkID} path segment).
func seedWebhookLink(t *testing.T, s *Server, q *dbtest.FakeQuerier, secret string) uuid.UUID {
	t.Helper()
	encrypted, err := s.cipher.Encrypt(secret)
	require.NoError(t, err)
	return q.SeedLinkedGitlabProject(encrypted).ID
}

// webhookRequest builds and executes a POST /webhooks/gitlab/{linkID}
// request through the router (never the handler directly, per
// docs/testing.md), with the headers GitLab sends on a real delivery.
func webhookRequest(t *testing.T, s *Server, linkID uuid.UUID, token, eventName, eventUUID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab/"+linkID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Gitlab-Token", token)
	}
	if eventName != "" {
		req.Header.Set("X-Gitlab-Event", eventName)
	}
	if eventUUID != "" {
		req.Header.Set("X-Gitlab-Event-UUID", eventUUID)
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestHandleGitlabWebhook_TokenVerification(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"correct secret", testWebhookSecret, http.StatusOK},
		{"wrong secret", "wrong-secret", http.StatusUnauthorized},
		{"missing token", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			linkID := seedWebhookLink(t, s, q, testWebhookSecret)

			rec := webhookRequest(t, s, linkID, tt.token, webhookevent.SupportedEventHeader, "delivery-1", []byte(issueHookPayload))

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.NotContains(t, rec.Body.String(), testWebhookSecret)
		})
	}
}

func TestHandleGitlabWebhook_UnknownLinkIsUnauthorized(t *testing.T) {
	s, _ := newTestServer(t)

	rec := webhookRequest(t, s, uuid.New(), testWebhookSecret, webhookevent.SupportedEventHeader, "delivery-1", []byte(issueHookPayload))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGitlabWebhook_DuplicateDeliveryIsIdempotent(t *testing.T) {
	s, q := newTestServer(t)
	linkID := seedWebhookLink(t, s, q, testWebhookSecret)

	rec1 := webhookRequest(t, s, linkID, testWebhookSecret, webhookevent.SupportedEventHeader, "delivery-dup", []byte(issueHookPayload))
	rec2 := webhookRequest(t, s, linkID, testWebhookSecret, webhookevent.SupportedEventHeader, "delivery-dup", []byte(issueHookPayload))

	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, http.StatusOK, rec2.Code)
	assert.Len(t, q.WebhookEventsForLink(linkID), 1, "a redelivered X-Gitlab-Event-UUID must not create a second row")
}

func TestHandleGitlabWebhook_UnsupportedEventIsRecordedAsSkipped(t *testing.T) {
	s, q := newTestServer(t)
	linkID := seedWebhookLink(t, s, q, testWebhookSecret)

	rec := webhookRequest(t, s, linkID, testWebhookSecret, "Push Hook", "delivery-push", []byte(`{}`))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	events := q.WebhookEventsForLink(linkID)
	require.Len(t, events, 1)
	assert.Equal(t, webhookevent.StatusSkipped, events[0].Status)
	assert.Equal(t, webhookevent.SkipReasonUnsupportedEvent, events[0].SkipReason)
}

func TestHandleGitlabWebhook_BodyTooLargeIsRejected(t *testing.T) {
	s, q := newTestServer(t)
	linkID := seedWebhookLink(t, s, q, testWebhookSecret)

	huge := bytes.Repeat([]byte("a"), webhookMaxBodyBytes+1)
	rec := webhookRequest(t, s, linkID, testWebhookSecret, webhookevent.SupportedEventHeader, "delivery-huge", huge)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Empty(t, q.WebhookEventsForLink(linkID), "an oversized delivery must never be recorded")
}

func TestHandleGitlabWebhook_ReceivedMetric_CountsByOutcome(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		eventName  string
		wantCode   int
		wantMetric string
	}{
		{"accepted issue event", testWebhookSecret, webhookevent.SupportedEventHeader, http.StatusOK, webhookMetricProcessed},
		{"unsupported event", testWebhookSecret, "Push Hook", http.StatusAccepted, webhookMetricSkipped},
		{"bad token", "wrong-secret", webhookevent.SupportedEventHeader, http.StatusUnauthorized, webhookMetricFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			linkID := seedWebhookLink(t, s, q, testWebhookSecret)
			before := testutil.ToFloat64(webhookEventsReceivedTotal.WithLabelValues(tt.wantMetric))

			rec := webhookRequest(t, s, linkID, tt.token, tt.eventName, "delivery-1", []byte(issueHookPayload))

			assert.Equal(t, tt.wantCode, rec.Code)
			after := testutil.ToFloat64(webhookEventsReceivedTotal.WithLabelValues(tt.wantMetric))
			assert.Equal(t, before+1, after, "expected the %q outcome counter to increment by one", tt.wantMetric)
		})
	}
}
