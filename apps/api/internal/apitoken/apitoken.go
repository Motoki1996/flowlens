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
//
// A token also carries scopes (read, and optionally write) and, once
// authenticated, resolves to an Auth: the rest of the app has no
// project-scoped notion of "acting user", so a bearer request acts as its
// token's project's owner.
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
	ErrInvalidScopes = errors.New("apitoken: scopes must be a non-empty subset of read, write")
	ErrNotFound      = errors.New("apitoken: not found")
	ErrTokenNotFound = errors.New("apitoken: token not found")
)

// Scope values a token can be issued with. ScopeWrite implies ScopeRead —
// normalizeScopes always expands a lone "write" to {read, write} — so a
// caller only ever needs to check the scope a specific operation requires.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// lastUsedAtUpdateInterval throttles how often a successful Authenticate
// call writes last_used_at, so a busy integration does not turn every
// request into a database write.
const lastUsedAtUpdateInterval = 5 * time.Minute

// rawTokenPrefix marks every issued token so it is identifiable by eye or by
// a secret-scanning tool (e.g. in a grep, a leaked log, or a git history
// scan) as a FlowLens API token. It is part of the value hashed into
// token_hash — hashing the prefix along with the random body costs nothing
// and keeps HashToken's input simply "the bearer value presented over the
// wire".
const rawTokenPrefix = "flt_"

// tokenPrefixDisplayChars is how many characters of the random body (beyond
// rawTokenPrefix) are kept in token_prefix, for a token list UI to tell
// issued tokens apart (e.g. "flt_a1b2c3d4") without storing anything that
// would help reconstruct the full token.
const tokenPrefixDisplayChars = 8

// tokenPrefixFor returns the leading slice of a raw token to store in
// token_prefix.
func tokenPrefixFor(rawToken string) string {
	n := len(rawTokenPrefix) + tokenPrefixDisplayChars
	if n > len(rawToken) {
		n = len(rawToken)
	}
	return rawToken[:n]
}

// APIToken is the API-facing representation of a project API token. It
// never carries the token's hash or raw value — the raw value exists only
// in Service.Create's return, immediately after issuance.
type APIToken struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"projectId"`
	Name        string     `json:"name"`
	Scopes      []string   `json:"scopes"`
	TokenPrefix string     `json:"tokenPrefix,omitempty"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// Auth is what a valid bearer token resolves to: the project it was issued
// for, the project's owner (the user a bearer-authenticated request acts
// as — see the package doc), and the scopes it was granted.
type Auth struct {
	ProjectID   uuid.UUID
	OwnerUserID uuid.UUID
	Scopes      []string
}

// HasScope reports whether the token this Auth was resolved from was
// granted scope s.
func (a Auth) HasScope(s string) bool {
	for _, scope := range a.Scopes {
		if scope == s {
			return true
		}
	}
	return false
}

// fromRow maps a database row to the domain model.
func fromRow(row db.ProjectApiToken) APIToken {
	t := APIToken{
		ID:        row.ID,
		ProjectID: row.ProjectID,
		Name:      row.Name,
		Scopes:    row.Scopes,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.TokenPrefix.Valid {
		t.TokenPrefix = row.TokenPrefix.String
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

// normalizeScopes validates raw against the read/write vocabulary and
// returns it in canonical order (read before write) with duplicates
// collapsed. A lone "write" is expanded to {read, write} since write implies
// read. An empty or unrecognized scope is ErrInvalidScopes.
func normalizeScopes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, ErrInvalidScopes
	}
	set := make(map[string]bool, len(raw))
	for _, s := range raw {
		switch s {
		case ScopeRead, ScopeWrite:
			set[s] = true
		default:
			return nil, ErrInvalidScopes
		}
	}
	if set[ScopeWrite] {
		set[ScopeRead] = true
	}
	scopes := make([]string, 0, 2)
	if set[ScopeRead] {
		scopes = append(scopes, ScopeRead)
	}
	if set[ScopeWrite] {
		scopes = append(scopes, ScopeWrite)
	}
	return scopes, nil
}

// Create issues a new API token for projectID and returns both the token
// record and the raw bearer value — the only place the raw value is ever
// available, mirroring auth.SessionService.Create. scopes must be a
// non-empty subset of ScopeRead/ScopeWrite (see normalizeScopes). expiresAt
// is optional; a nil value means the token never expires.
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, name string, scopes []string, expiresAt *time.Time) (APIToken, string, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return APIToken{}, "", ErrNotFound
		}
		return APIToken{}, "", fmt.Errorf("apitoken: create: %w", err)
	}

	normalizedName, err := normalizeName(name)
	if err != nil {
		return APIToken{}, "", err
	}
	normalizedScopes, err := normalizeScopes(scopes)
	if err != nil {
		return APIToken{}, "", err
	}

	body, err := auth.NewOpaqueToken()
	if err != nil {
		return APIToken{}, "", fmt.Errorf("apitoken: create: %w", err)
	}
	rawToken := rawTokenPrefix + body

	var expiresAtParam pgtype.Timestamptz
	if expiresAt != nil {
		expiresAtParam = pgtype.Timestamptz{Time: *expiresAt, Valid: true}
	}

	row, err := s.q.CreateProjectAPIToken(ctx, db.CreateProjectAPITokenParams{
		ProjectID:   projectID,
		Name:        normalizedName,
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: pgtype.Text{String: tokenPrefixFor(rawToken), Valid: true},
		Scopes:      normalizedScopes,
		ExpiresAt:   expiresAtParam,
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

// Authenticate resolves a raw bearer token to the Auth it grants — the
// project it was issued for, that project's owner (a bearer request acts as
// this user; see Auth), and its scopes. It returns ErrTokenNotFound for a
// token that is unknown, expired, or revoked — the lookup is a single
// indexed hash-equality match (auth.HashToken then
// GetProjectAPITokenByTokenHash), so comparison time never depends on how
// much of a guessed token happens to be correct.
//
// On success it opportunistically refreshes last_used_at, throttled to
// lastUsedAtUpdateInterval so a busy caller does not turn every request into
// a write.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (Auth, error) {
	if rawToken == "" {
		return Auth{}, ErrTokenNotFound
	}

	row, err := s.q.GetProjectAPITokenByTokenHash(ctx, auth.HashToken(rawToken))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Auth{}, ErrTokenNotFound
		}
		return Auth{}, fmt.Errorf("apitoken: authenticate: %w", err)
	}

	if !row.LastUsedAt.Valid || time.Since(row.LastUsedAt.Time) > lastUsedAtUpdateInterval {
		if err := s.q.UpdateProjectAPITokenLastUsedAt(ctx, db.UpdateProjectAPITokenLastUsedAtParams{
			ID:         row.ID,
			LastUsedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}); err != nil {
			return Auth{}, fmt.Errorf("apitoken: authenticate: update last used: %w", err)
		}
	}
	return Auth{ProjectID: row.ProjectID, OwnerUserID: row.OwnerUserID, Scopes: row.Scopes}, nil
}
