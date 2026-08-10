// Package webhookevent verifies and records inbound GitLab issue and note
// webhook deliveries (docs/plans/issue-sync.md, "Inbound"; ADR-0008; note
// events since #104). Service never
// applies an event to a task — Record only stores it (status='pending' or
// 'skipped') and returns; applying pending events to tasks is
// internal/webhookapply's job. This package also owns the troubleshooting
// read side (issue #26): listing and inspecting recorded events and
// retrying a failed one, plus the retention cleanup worker.
package webhookevent

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrUnauthorized covers every reason a delivery is rejected before being
// recorded: an unknown linkID, a link whose webhook was never registered
// (nothing to compare against), and a token that does not match. Service
// never distinguishes between them in what it returns, so a caller can
// never leak which case occurred (ADR-0008).
var ErrUnauthorized = errors.New("webhookevent: unauthorized")

// Event statuses stored in webhook_events.status by this package. A
// worker (a later phase) moves 'pending' rows on to 'processed' or 'failed'.
const (
	StatusPending = "pending"
	StatusSkipped = "skipped"
)

// Skip reasons stored in webhook_events.skip_reason by this package.
const SkipReasonUnsupportedEvent = "unsupported_event"

// IssueEventHeader and NoteEventHeader are the X-Gitlab-Event header values
// this phase applies — issue events (docs/plans/issue-sync.md, "Inbound")
// and, since #104, note events (GitLab CE's discussion/comment webhook).
// Any other event is still recorded, but as StatusSkipped /
// SkipReasonUnsupportedEvent rather than StatusPending.
const (
	IssueEventHeader = "Issue Hook"
	NoteEventHeader  = "Note Hook"

	// SupportedEventHeader is IssueEventHeader, kept as an alias so existing
	// callers/tests built around "the one supported event" still compile.
	SupportedEventHeader = IssueEventHeader
)

// IsSupportedEventHeader reports whether eventName is an X-Gitlab-Event
// header value this phase applies.
func IsSupportedEventHeader(eventName string) bool {
	return eventName == IssueEventHeader || eventName == NoteEventHeader
}

// Service verifies webhook deliveries against a linked GitLab project's
// secret and records them.
type Service struct {
	q      db.Querier
	cipher *crypto.Cipher
}

// NewService constructs a webhookevent Service. cipher decrypts each link's
// webhook secret to compare against an inbound delivery's X-Gitlab-Token.
func NewService(q db.Querier, cipher *crypto.Cipher) *Service {
	return &Service{q: q, cipher: cipher}
}

// VerifyToken decrypts linkID's webhook secret and compares it against
// token (the X-Gitlab-Token header) in constant time via hmac.Equal. It
// returns ErrUnauthorized for every failure case, never distinguishing them.
func (s *Service) VerifyToken(ctx context.Context, linkID uuid.UUID, token string) error {
	link, err := s.q.GetLinkedGitlabProjectByID(ctx, linkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return fmt.Errorf("webhookevent: verify token: %w", err)
	}
	if len(link.EncryptedWebhookSecret) == 0 || token == "" {
		return ErrUnauthorized
	}

	secret, err := s.cipher.Decrypt(link.EncryptedWebhookSecret)
	if err != nil {
		return fmt.Errorf("webhookevent: decrypt webhook secret: %w", err)
	}
	if !hmac.Equal([]byte(token), []byte(secret)) {
		return ErrUnauthorized
	}
	return nil
}

// RecordParams holds the fields extracted from a verified delivery.
// GitlabIssueIID and GitlabUpdatedAt are zero when the payload could not be
// parsed or the event is not an issue event; the row is still recorded.
type RecordParams struct {
	LinkedGitlabProjectID uuid.UUID
	DeliveryUUID          string
	EventName             string
	ObjectKind            string
	GitlabIssueIID        int64
	Payload               []byte
	GitlabUpdatedAt       time.Time
	Status                string
	SkipReason            string
}

// Record stores a verified delivery. A duplicate delivery — the same
// LinkedGitlabProjectID + DeliveryUUID as an already-recorded row — is a
// no-op: the UNIQUE constraint backing this makes GitLab's at-least-once
// redelivery idempotent, so the caller responds identically whether this
// call inserted a new row or found one already there.
func (s *Service) Record(ctx context.Context, params RecordParams) error {
	_, err := s.q.CreateWebhookEvent(ctx, db.CreateWebhookEventParams{
		LinkedGitlabProjectID: params.LinkedGitlabProjectID,
		DeliveryUuid:          params.DeliveryUUID,
		EventName:             params.EventName,
		ObjectKind:            params.ObjectKind,
		GitlabIssueIid:        issueIIDToInt8(params.GitlabIssueIID),
		Payload:               params.Payload,
		GitlabUpdatedAt:       toTimestamptz(params.GitlabUpdatedAt),
		Status:                params.Status,
		SkipReason:            params.SkipReason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("webhookevent: record: %w", err)
	}
	return nil
}

func issueIIDToInt8(iid int64) pgtype.Int8 {
	if iid == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: iid, Valid: true}
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// Additional webhook_events.status values a recorded event can reach once
// internal/webhookapply applies it — recorded here alongside StatusPending
// and StatusSkipped so every valid value (and the CHECK constraint they
// mirror) lives in one place, even though this package's own Record method
// never writes them itself.
const (
	StatusProcessed = "processed"
	StatusFailed    = "failed"
)

// Sentinel errors returned by the query/retry side of Service. Handlers map
// these to HTTP status codes.
var (
	// ErrNotFound covers an unknown linkID/eventID and a linkID or eventID
	// that exists but belongs to another user's project — the two are never
	// distinguished, the same as every other project-scoped resource.
	ErrNotFound = errors.New("webhookevent: not found")
	// ErrNotFailed is returned by Retry when the named event is not
	// currently 'failed' (already pending/processed/skipped, or a
	// concurrent retry already moved it).
	ErrNotFailed = errors.New("webhookevent: event is not in a failed state")
)

// Pagination defaults and bounds for List.
const (
	DefaultPerPage = 20
	MaxPerPage     = 100
)

// Event is the API-facing representation of one recorded webhook delivery,
// without its payload — the list view never returns raw GitLab payloads by
// default (docs/plans/issue-sync.md, "webhook_events"); Get is the only way
// to retrieve one.
type Event struct {
	ID                    uuid.UUID  `json:"id"`
	LinkedGitlabProjectID uuid.UUID  `json:"linkedGitlabProjectId"`
	EventName             string     `json:"eventName"`
	ObjectKind            string     `json:"objectKind"`
	GitlabIssueIID        *int64     `json:"gitlabIssueIid"`
	Status                string     `json:"status"`
	SkipReason            string     `json:"skipReason"`
	ErrorMessage          string     `json:"errorMessage"`
	ReceivedAt            time.Time  `json:"receivedAt"`
	ProcessedAt           *time.Time `json:"processedAt"`
}

// EventDetail is Event plus its raw payload, returned only by Get.
type EventDetail struct {
	Event
	Payload json.RawMessage `json:"payload"`
}

func fromEventRow(row db.WebhookEvent) Event {
	e := Event{
		ID:                    row.ID,
		LinkedGitlabProjectID: row.LinkedGitlabProjectID,
		EventName:             row.EventName,
		ObjectKind:            row.ObjectKind,
		Status:                row.Status,
		SkipReason:            row.SkipReason,
		ErrorMessage:          row.ErrorMessage,
		ReceivedAt:            row.ReceivedAt.Time,
	}
	if row.GitlabIssueIid.Valid {
		iid := row.GitlabIssueIid.Int64
		e.GitlabIssueIID = &iid
	}
	if row.ProcessedAt.Valid {
		t := row.ProcessedAt.Time
		e.ProcessedAt = &t
	}
	return e
}

// ListParams narrows List's page of events.
type ListParams struct {
	// Status filters to one status ("pending", "processed", "skipped",
	// "failed"); empty lists every status.
	Status  string
	Page    int
	PerPage int
}

// EventsPage is one page of List's results, newest first.
type EventsPage struct {
	Events   []Event
	NextPage int // 0 when there is no next page
}

// List returns a page of linkID's recorded webhook events, newest first,
// scoped to ownerID. It returns ErrNotFound if linkID does not exist or
// belongs to another user.
func (s *Service) List(ctx context.Context, ownerID, linkID uuid.UUID, params ListParams) (EventsPage, error) {
	if _, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: linkID, OwnerUserID: ownerID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EventsPage{}, ErrNotFound
		}
		return EventsPage{}, fmt.Errorf("webhookevent: list: %w", err)
	}

	page := params.Page
	if page < 1 {
		page = 1
	}
	perPage := params.PerPage
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}

	// Fetch one extra row to tell whether another page follows, the same
	// "peek" pattern internal/linkedproject's GitLab-side pagination uses.
	rows, err := s.q.ListWebhookEventsByLinkedGitlabProjectID(ctx, db.ListWebhookEventsByLinkedGitlabProjectIDParams{
		LinkedGitlabProjectID: linkID,
		Status:                params.Status,
		LimitCount:            int32(perPage + 1),
		OffsetCount:           int32((page - 1) * perPage),
	})
	if err != nil {
		return EventsPage{}, fmt.Errorf("webhookevent: list: %w", err)
	}

	nextPage := 0
	if len(rows) > perPage {
		rows = rows[:perPage]
		nextPage = page + 1
	}

	events := make([]Event, len(rows))
	for i, row := range rows {
		events[i] = fromEventRow(row)
	}
	return EventsPage{Events: events, NextPage: nextPage}, nil
}

// Get returns one recorded webhook event, including its raw payload, scoped
// to ownerID. It returns ErrNotFound if linkID does not exist or belongs to
// another user, or if eventID does not exist or does not belong to linkID.
func (s *Service) Get(ctx context.Context, ownerID, linkID, eventID uuid.UUID) (EventDetail, error) {
	if _, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: linkID, OwnerUserID: ownerID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EventDetail{}, ErrNotFound
		}
		return EventDetail{}, fmt.Errorf("webhookevent: get: %w", err)
	}

	row, err := s.q.GetWebhookEventByLinkedGitlabProjectIDAndID(ctx, db.GetWebhookEventByLinkedGitlabProjectIDAndIDParams{
		ID:                    eventID,
		LinkedGitlabProjectID: linkID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EventDetail{}, ErrNotFound
		}
		return EventDetail{}, fmt.Errorf("webhookevent: get: %w", err)
	}
	return EventDetail{Event: fromEventRow(row), Payload: json.RawMessage(row.Payload)}, nil
}

// Retry puts a 'failed' event back to 'pending' so internal/webhookapply's
// worker picks it up again, scoped to ownerID. It returns ErrNotFound if
// linkID does not exist or belongs to another user, or if eventID does not
// exist or does not belong to linkID; ErrNotFailed if eventID exists but is
// not currently 'failed'.
func (s *Service) Retry(ctx context.Context, ownerID, linkID, eventID uuid.UUID) (Event, error) {
	if _, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: linkID, OwnerUserID: ownerID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Event{}, ErrNotFound
		}
		return Event{}, fmt.Errorf("webhookevent: retry: %w", err)
	}

	existing, err := s.q.GetWebhookEventByLinkedGitlabProjectIDAndID(ctx, db.GetWebhookEventByLinkedGitlabProjectIDAndIDParams{
		ID:                    eventID,
		LinkedGitlabProjectID: linkID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Event{}, ErrNotFound
		}
		return Event{}, fmt.Errorf("webhookevent: retry: %w", err)
	}
	if existing.Status != StatusFailed {
		return Event{}, ErrNotFailed
	}

	row, err := s.q.RetryWebhookEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost a race against a concurrent retry (or the apply worker) that
			// already moved the event out of 'failed' between the read above
			// and this write.
			return Event{}, ErrNotFailed
		}
		return Event{}, fmt.Errorf("webhookevent: retry: %w", err)
	}
	return fromEventRow(row), nil
}
