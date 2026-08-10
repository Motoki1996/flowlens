// Package notification implements the daily digest notifications described
// in issue #109: a per-project webhook that reports overdue tasks, tasks
// due within 24h, and failed sync jobs / webhook events, so a team doesn't
// have to keep visiting /dashboard to notice them. Delivery is an outgoing
// webhook, not email — agreed on the issue, since it needs no SMTP setup
// and plugs straight into Slack via an Incoming Webhook URL.
package notification

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sentinel errors returned by Service. internal/http maps these to status
// codes.
var (
	ErrInvalidWebhookURL = errors.New("notification: webhook_url must be an absolute http(s) URL")
	ErrInvalidSendHour   = errors.New("notification: send_hour must be between 0 and 23")
	ErrNotFound          = errors.New("notification: not found")
	ErrForbidden         = errors.New("notification: forbidden")
)

// DefaultSendHour is the UTC hour a project's digest sends at until its
// owner configures otherwise, matching the notification_settings column
// default.
const DefaultSendHour = 9

// Settings is a project's notification configuration.
type Settings struct {
	ProjectID  uuid.UUID `json:"projectId"`
	WebhookURL string    `json:"webhookUrl"`
	Enabled    bool      `json:"enabled"`
	SendHour   int       `json:"sendHour"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Service manages a project's notification settings (the CRUD side of
// issue #109). Building and sending the daily digest itself is Worker's
// job, not Service's — Worker has no acting user to authorize against.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a notification Service.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

func fromRow(row db.NotificationSetting) Settings {
	return Settings{
		ProjectID:  row.ProjectID,
		WebhookURL: row.WebhookUrl,
		Enabled:    row.Enabled,
		SendHour:   int(row.SendHour),
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
}

// normalizeWebhookURL trims raw and requires an absolute http(s) URL.
func normalizeWebhookURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalidWebhookURL
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrInvalidWebhookURL
	}
	return trimmed, nil
}

// Save validates and upserts projectID's notification settings. A
// webhook_url is only required when enabled is true, so a project can be
// switched off without clearing its configured URL.
func (s *Service) Save(ctx context.Context, ownerID, projectID uuid.UUID, webhookURL string, enabled bool, sendHour int) (Settings, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Settings{}, err
	}
	if sendHour < 0 || sendHour > 23 {
		return Settings{}, ErrInvalidSendHour
	}

	normalized := ""
	if webhookURL != "" || enabled {
		var err error
		normalized, err = normalizeWebhookURL(webhookURL)
		if err != nil {
			return Settings{}, err
		}
	}

	row, err := s.q.UpsertNotificationSettings(ctx, db.UpsertNotificationSettingsParams{
		ProjectID:  projectID,
		WebhookUrl: normalized,
		Enabled:    enabled,
		SendHour:   int32(sendHour),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("notification: save: %w", err)
	}
	return fromRow(row), nil
}

// Get returns projectID's notification settings, or its unconfigured
// defaults (disabled, DefaultSendHour) if it has never been saved —
// settings conceptually always exist, just possibly unset.
func (s *Service) Get(ctx context.Context, ownerID, projectID uuid.UUID) (Settings, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Settings{}, err
	}
	row, err := s.q.GetNotificationSettingsForOwner(ctx, db.GetNotificationSettingsForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Settings{ProjectID: projectID, SendHour: DefaultSendHour}, nil
		}
		return Settings{}, fmt.Errorf("notification: get: %w", err)
	}
	return fromRow(row), nil
}

// authorizeOwner requires ownerID to hold project.RoleOwner on projectID:
// notification settings carry an outbound URL a lesser role should not be
// able to redirect, the same reasoning gitlabconn applies to its credential.
func (s *Service) authorizeOwner(ctx context.Context, ownerID, projectID uuid.UUID) error {
	err := s.projects.Authorize(ctx, ownerID, projectID, project.RoleOwner)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return ErrForbidden
	default:
		return fmt.Errorf("notification: authorize: %w", err)
	}
}
