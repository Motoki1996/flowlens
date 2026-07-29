// Package webhookevent verifies and records inbound GitLab issue webhook
// deliveries (docs/plans/issue-sync.md, "Inbound"; ADR-0008). Service never
// applies an event to a task — Record only stores it (status='pending' or
// 'skipped') and returns; applying pending events to tasks is the outbox
// worker's job, a later phase.
package webhookevent

import (
	"context"
	"crypto/hmac"
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

// SupportedEventHeader is the only X-Gitlab-Event header value this phase
// applies. Any other event is still recorded, but as StatusSkipped /
// SkipReasonUnsupportedEvent rather than StatusPending.
const SupportedEventHeader = "Issue Hook"

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
