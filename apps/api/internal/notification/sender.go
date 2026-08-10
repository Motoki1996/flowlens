package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Sender delivers a built Digest to a project's configured destination.
// Production code uses HTTPSender; tests use FakeSender (docs/testing.md:
// prefer a stateful fake over a call-expectation mock).
type Sender interface {
	Send(ctx context.Context, webhookURL string, digest Digest) error
}

// digestPayload is the JSON body posted to webhookURL. It stays flat and
// count-first so a Slack-Incoming-Webhook-compatible relay (or a human
// skimming a debug log) doesn't need to unpack the full task/job rows to
// see whether anything needs attention.
type digestPayload struct {
	ProjectID          string   `json:"projectId"`
	Date               string   `json:"date"`
	OverdueCount       int      `json:"overdueCount"`
	OverdueTitles      []string `json:"overdueTaskTitles"`
	DueSoonCount       int      `json:"dueSoonCount"`
	DueSoonTitles      []string `json:"dueSoonTaskTitles"`
	FailedSyncJobCount int      `json:"failedSyncJobCount"`
	FailedWebhookCount int      `json:"failedWebhookEventCount"`
}

func toPayload(digest Digest) digestPayload {
	overdueTitles := make([]string, len(digest.Overdue))
	for i, t := range digest.Overdue {
		overdueTitles[i] = t.Title
	}
	dueSoonTitles := make([]string, len(digest.DueSoon))
	for i, t := range digest.DueSoon {
		dueSoonTitles[i] = t.Title
	}
	return digestPayload{
		ProjectID:          digest.ProjectID.String(),
		Date:               digest.Date.Format("2006-01-02"),
		OverdueCount:       len(digest.Overdue),
		OverdueTitles:      overdueTitles,
		DueSoonCount:       len(digest.DueSoon),
		DueSoonTitles:      dueSoonTitles,
		FailedSyncJobCount: len(digest.FailedSyncJobs),
		FailedWebhookCount: len(digest.FailedWebhookEvents),
	}
}

// HTTPSenderTimeout bounds a single webhook POST, so one unreachable
// destination can never stall the worker's sweep of other projects.
const HTTPSenderTimeout = 10 * time.Second

// HTTPSender posts a digest as JSON to webhookURL.
type HTTPSender struct {
	client *http.Client
}

// NewHTTPSender constructs an HTTPSender with HTTPSenderTimeout.
func NewHTTPSender() *HTTPSender {
	return &HTTPSender{client: &http.Client{Timeout: HTTPSenderTimeout}}
}

// Send POSTs digest as JSON to webhookURL and treats any non-2xx response as
// a failure.
func (s *HTTPSender) Send(ctx context.Context, webhookURL string, digest Digest) error {
	body, err := json.Marshal(toPayload(digest))
	if err != nil {
		return fmt.Errorf("notification: marshal digest: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notification: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("notification: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification: send: webhook returned status %d", resp.StatusCode)
	}
	return nil
}
