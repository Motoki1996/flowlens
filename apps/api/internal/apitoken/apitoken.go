// Package apitoken contains the domain model and service that manage
// project-scoped API tokens: opaque bearer credentials external callers
// (e.g. an AI agent) use to reach one project's resources without a user
// session, per docs/plans/issue-sync.md ("AI-facing").
//
// Tokens are issued and stored the same way internal/auth.SessionService
// handles sessions: the raw token is returned only once, at creation, and
// the database keeps just its SHA-256 hash (auth.HashToken). project_api_tokens
// has no owner column of its own; every management method takes the acting
// user's ID and verifies project ownership through project.Service.Get or a
// SQL JOIN, the same way internal/backlog does. A token belonging to
// another user's project is indistinguishable from a missing one
// (ErrNotFound).
package apitoken

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is returned both when a token/project does not exist
// and when it belongs to another user. ErrTokenNotFound is returned by
// Authenticate for a token that is unknown, expired, or revoked (deleted) —
// the three cases are never distinguished, so a caller can never learn
// which occurred.
var (
	ErrInvalidName   = errors.New("apitoken: name must be 1-100 characters")
	ErrNotFound      = errors.New("apitoken: not found")
	ErrTokenNotFound = errors.New("apitoken: token not found")
)

// lastUsedAtUpdateInterval throttles how often a successful Authenticate
// call writes last_used_at, so a busy integration does not turn every
// request into a database write.
const lastUsedAtUpdateInterval = 5 * time.Minute

// APIToken is the API-facing representation of a project API token. It
// never carries the token's hash or raw value — the raw value exists only
// in Service.Create's return, immediately after issuance.
type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	ProjectID  uuid.UUID  `json:"projectId"`
	Name       string     `json:"name"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.ProjectApiToken) APIToken {
	t := APIToken{
		ID:        row.ID,
		ProjectID: row.ProjectID,
		Name:      row.Name,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.LastUsedAt.Valid {
		lastUsedAt := row.LastUsedAt.Time
		t.LastUsedAt = &lastUsedAt
	}
	if row.ExpiresAt.Valid {
		expiresAt := row.ExpiresAt.Time
		t.ExpiresAt = &expiresAt
	}
	return t
}

// Service manages API tokens for projects owned by a single user.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs an apitoken Service. projects is used to verify
// project ownership before any project-scoped operation.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

// normalizeName trims raw and enforces the 1-100 character rule.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ErrInvalidName
	}
	return name, nil
}

// Create issues a new API token for projectID and returns both the token
// record and the raw bearer value — the only place the raw value is ever
// available, mirroring auth.SessionService.Create. expiresAt is optional;
// a nil value means the token never expires.
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, name string, expiresAt *time.Time) (APIToken, string, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return APIToken{}, "", ErrNotFound
		}
		return APIToken{}, "", fmt.Errorf("apitoken: create: %w", err)
	}

	normalized, err := normalizeName(name)
	if err != nil {
		return APIToken{}, "", err
	}

	rawToken, err := auth.NewOpaqueToken()
	if err != nil {
		return APIToken{}, "", fmt.Errorf("apitoken: create: %w", err)
	}

	var expiresAtParam pgtype.Timestamptz
	if expiresAt != nil {
		expiresAtParam = pgtype.Timestamptz{Time: *expiresAt, Valid: true}
	}

	row, err := s.q.CreateProjectAPIToken(ctx, db.CreateProjectAPITokenParams{
		ProjectID: projectID,
		Name:      normalized,
		TokenHash: auth.HashToken(rawToken),
		ExpiresAt: expiresAtParam,
	})
	if err != nil {
		return APIToken{}, "", fmt.Errorf("apitoken: create: %w", err)
	}
	return fromRow(row), rawToken, nil
}

// List returns every API token issued for projectID, newest first. It
// returns ErrNotFound if projectID does not exist or belongs to another
// user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID) ([]APIToken, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("apitoken: list: %w", err)
	}

	rows, err := s.q.ListProjectAPITokensByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("apitoken: list: %w", err)
	}
	out := make([]APIToken, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Delete revokes an API token. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted.
func (s *Service) Delete(ctx context.Context, ownerID, tokenID uuid.UUID) error {
	affected, err := s.q.DeleteProjectAPITokenForOwner(ctx, db.DeleteProjectAPITokenForOwnerParams{
		ID:          tokenID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return fmt.Errorf("apitoken: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate resolves a raw bearer token to the ID of the project it was
// issued for. It returns ErrTokenNotFound for a token that is unknown,
// expired, or revoked — the lookup is a single indexed hash-equality match
// (auth.HashToken then GetProjectAPITokenByTokenHash), so comparison time
// never depends on how much of a guessed token happens to be correct.
//
// On success it opportunistically refreshes last_used_at, throttled to
// lastUsedAtUpdateInterval so a busy caller does not turn every request into
// a write.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (uuid.UUID, error) {
	if rawToken == "" {
		return uuid.Nil, ErrTokenNotFound
	}

	row, err := s.q.GetProjectAPITokenByTokenHash(ctx, auth.HashToken(rawToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrTokenNotFound
		}
		return uuid.Nil, fmt.Errorf("apitoken: authenticate: %w", err)
	}

	if !row.LastUsedAt.Valid || time.Since(row.LastUsedAt.Time) > lastUsedAtUpdateInterval {
		if err := s.q.UpdateProjectAPITokenLastUsedAt(ctx, db.UpdateProjectAPITokenLastUsedAtParams{
			ID:         row.ID,
			LastUsedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			return uuid.Nil, fmt.Errorf("apitoken: authenticate: update last used: %w", err)
		}
	}
	return row.ProjectID, nil
}
