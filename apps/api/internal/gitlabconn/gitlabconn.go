// Package gitlabconn contains the domain model and service that manage a
// project's connection to a GitLab CE instance: the base URL and personal
// access token used for issue sync.
//
// The access token is encrypted at rest (internal/crypto) and never leaves
// this package or crosses into an API response in plaintext — Connection,
// the only type Service returns, carries just the token's last four
// characters. Every HTTP-facing method (Save/Get/Test/Delete) requires
// project.RoleOwner via authorizeOwner, since the connection carries a
// credential (issue #99); a caller with no membership row at all gets
// ErrNotFound, one with a lesser role gets ErrForbidden.
package gitlabconn

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is returned both when a connection/project does not
// exist and when the caller has no project_members row for it at all.
// ErrForbidden means the caller is a member but not owner: every
// HTTP-facing method here (Save/Get/Test/Delete) is owner-minimum, since the
// connection carries a credential — see project.RoleOwner and issue #99.
var (
	ErrInvalidBaseURL = errors.New("gitlabconn: base_url must be an absolute http(s) URL")
	ErrTokenRequired  = errors.New("gitlabconn: personal access token is required")
	ErrNotFound       = errors.New("gitlabconn: not found")
	ErrForbidden      = errors.New("gitlabconn: forbidden")

	// ErrUnreachable means the GitLab instance could not be reached at all
	// (DNS/connection failure, timeout, or an unexpected non-auth status).
	ErrUnreachable = errors.New("gitlabconn: gitlab instance unreachable")
	// ErrTokenInvalid means the instance was reached but rejected the token
	// (401).
	ErrTokenInvalid = errors.New("gitlabconn: personal access token is invalid")
	// ErrInsufficientScope means the instance was reached and the token was
	// accepted, but lacks the permissions required (403).
	ErrInsufficientScope = errors.New("gitlabconn: personal access token lacks required permissions")
)

// gitlabUnauthorizedStatus and gitlabForbiddenStatus are the GitLab CE API
// status codes that distinguish an invalid token from an under-scoped one.
const (
	gitlabUnauthorizedStatus = 401
	gitlabForbiddenStatus    = 403
)

// tokenLastFourLen is how many trailing characters of the access token are
// safe to surface in API responses.
const tokenLastFourLen = 4

// Connection is the API-facing representation of a project's GitLab
// connection. It never carries the access token itself.
type Connection struct {
	ProjectID           uuid.UUID  `json:"projectId"`
	BaseURL             string     `json:"baseUrl"`
	TokenLastFour       string     `json:"tokenLastFour"`
	TokenGitlabUserID   *int64     `json:"tokenGitlabUserId,omitempty"`
	TokenGitlabUsername string     `json:"tokenGitlabUsername"`
	Verified            bool       `json:"verified"`
	LastVerifiedAt      *time.Time `json:"lastVerifiedAt,omitempty"`
	LastVerifyError     string     `json:"lastVerifyError,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// ClientFactory builds a gitlab.Client bound to baseURL. Production code
// points this at gitlab.NewHTTPClient; tests inject a factory that returns
// a gitlab.FakeClient.
type ClientFactory func(baseURL string) gitlab.Client

// Service manages GitLab connections for projects owned by a single user.
type Service struct {
	q             db.Querier
	projects      *project.Service
	cipher        *crypto.Cipher
	clientFactory ClientFactory
}

// NewService constructs a gitlabconn Service. cipher encrypts/decrypts the
// access token at rest; clientFactory builds the gitlab.Client used to
// verify a token against baseURL.
func NewService(q db.Querier, projects *project.Service, cipher *crypto.Cipher, clientFactory ClientFactory) *Service {
	return &Service{q: q, projects: projects, cipher: cipher, clientFactory: clientFactory}
}

// normalizeBaseURL trims raw, requires an http(s) scheme and host, and
// strips any trailing slash, query, or fragment so the same instance always
// normalizes to the same stored value.
func normalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalidBaseURL
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrInvalidBaseURL
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

// classifyVerifyError maps a gitlab.Client verification error to a
// sentinel that distinguishes unreachable, invalid-token, and
// insufficient-scope outcomes, as required by the "save" and "test" flows.
func classifyVerifyError(err error) error {
	var apiErr *gitlab.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case gitlabUnauthorizedStatus:
			return ErrTokenInvalid
		case gitlabForbiddenStatus:
			return ErrInsufficientScope
		}
	}
	return ErrUnreachable
}

// fromRow maps a database row and the token's plaintext (for the last-four
// display value only) to the domain model.
func fromRow(row db.GitlabConnection, tokenLastFour string) Connection {
	c := Connection{
		ProjectID:           row.ProjectID,
		BaseURL:             row.BaseUrl,
		TokenLastFour:       tokenLastFour,
		TokenGitlabUsername: row.TokenGitlabUsername,
		LastVerifyError:     row.LastVerifyError,
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}
	if row.TokenGitlabUserID.Valid {
		id := row.TokenGitlabUserID.Int64
		c.TokenGitlabUserID = &id
	}
	if row.LastVerifiedAt.Valid {
		t := row.LastVerifiedAt.Time
		c.LastVerifiedAt = &t
		c.Verified = row.LastVerifyError == ""
	}
	return c
}

// lastFour returns the trailing tokenLastFourLen characters of token, or
// the whole token if shorter.
func lastFour(token string) string {
	if len(token) <= tokenLastFourLen {
		return token
	}
	return token[len(token)-tokenLastFourLen:]
}

// Save validates baseURL and token, verifies the token against the GitLab
// instance, and only then encrypts and stores it. A verification failure
// leaves any existing connection untouched and returns a classified error
// (ErrUnreachable, ErrTokenInvalid, ErrInsufficientScope) — nothing is
// written to the database.
func (s *Service) Save(ctx context.Context, ownerID, projectID uuid.UUID, rawBaseURL, token string) (Connection, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Connection{}, err
	}

	baseURL, err := normalizeBaseURL(rawBaseURL)
	if err != nil {
		return Connection{}, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Connection{}, ErrTokenRequired
	}

	user, err := s.clientFactory(baseURL).GetAuthenticatedUser(ctx, token)
	if err != nil {
		return Connection{}, classifyVerifyError(err)
	}

	encrypted, err := s.cipher.Encrypt(token)
	if err != nil {
		return Connection{}, fmt.Errorf("gitlabconn: encrypt token: %w", err)
	}

	row, err := s.q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:           projectID,
		BaseUrl:             baseURL,
		EncryptedToken:      encrypted,
		TokenGitlabUserID:   pgtype.Int8{Int64: user.ID, Valid: true},
		TokenGitlabUsername: user.Username,
	})
	if err != nil {
		return Connection{}, fmt.Errorf("gitlabconn: save: %w", err)
	}
	return fromRow(row, lastFour(token)), nil
}

// Get returns the stored connection for projectID, scoped to ownerID. The
// returned Connection never carries the token itself, only its last four
// characters (decrypted transiently to compute them, never returned or
// logged as a whole).
func (s *Service) Get(ctx context.Context, ownerID, projectID uuid.UUID) (Connection, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Connection{}, err
	}
	row, err := s.getRow(ctx, ownerID, projectID)
	if err != nil {
		return Connection{}, err
	}
	token, err := s.cipher.Decrypt(row.EncryptedToken)
	if err != nil {
		return Connection{}, fmt.Errorf("gitlabconn: get: %w", err)
	}
	return fromRow(row, lastFour(token)), nil
}

// Test re-verifies a stored connection's token against its GitLab instance
// and persists the outcome (token_gitlab_user_id/username, last_verified_at,
// last_verify_error), the same fields Save records. It distinguishes
// unreachable, invalid-token, and insufficient-scope failures.
func (s *Service) Test(ctx context.Context, ownerID, projectID uuid.UUID) (Connection, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Connection{}, err
	}
	row, err := s.getRow(ctx, ownerID, projectID)
	if err != nil {
		return Connection{}, err
	}
	token, err := s.cipher.Decrypt(row.EncryptedToken)
	if err != nil {
		return Connection{}, fmt.Errorf("gitlabconn: test: %w", err)
	}

	user, verifyErr := s.clientFactory(row.BaseUrl).GetAuthenticatedUser(ctx, token)

	params := db.UpdateGitlabConnectionVerificationForOwnerParams{
		ProjectID:           projectID,
		OwnerUserID:         ownerID,
		TokenGitlabUserID:   row.TokenGitlabUserID,
		TokenGitlabUsername: row.TokenGitlabUsername,
		LastVerifiedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	var classified error
	if verifyErr != nil {
		classified = classifyVerifyError(verifyErr)
		params.LastVerifyError = classified.Error()
	} else {
		params.TokenGitlabUserID = pgtype.Int8{Int64: user.ID, Valid: true}
		params.TokenGitlabUsername = user.Username
		params.LastVerifyError = ""
	}

	updated, err := s.q.UpdateGitlabConnectionVerificationForOwner(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, ErrNotFound
		}
		return Connection{}, fmt.Errorf("gitlabconn: test: %w", err)
	}
	if classified != nil {
		return Connection{}, classified
	}
	return fromRow(updated, lastFour(token)), nil
}

// Delete removes projectID's GitLab connection. Linked GitLab projects
// cascade-delete with it (schema FK); the caller (internal/http) unlinks
// each one via linkedproject.Service.Delete first, which deregisters its
// webhook on the GitLab side (issue #18) before this runs, so the cascade
// itself never needs to reach GitLab. Ownership is enforced by the query,
// so a non-owner gets ErrNotFound and nothing is deleted.
func (s *Service) Delete(ctx context.Context, ownerID, projectID uuid.UUID) error {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return err
	}
	affected, err := s.q.DeleteGitlabConnectionForOwner(ctx, db.DeleteGitlabConnectionForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return fmt.Errorf("gitlabconn: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Dial returns a ready gitlab.Client for projectID's connection, the
// decrypted personal access token to call it with, and the connection's own
// ID. It exists for packages like internal/linkedproject that need to call
// the GitLab API directly rather than go through Service's own methods.
func (s *Service) Dial(ctx context.Context, ownerID, projectID uuid.UUID) (gitlab.Client, string, uuid.UUID, error) {
	row, err := s.getRow(ctx, ownerID, projectID)
	if err != nil {
		return nil, "", uuid.Nil, err
	}
	token, err := s.cipher.Decrypt(row.EncryptedToken)
	if err != nil {
		return nil, "", uuid.Nil, fmt.Errorf("gitlabconn: dial: %w", err)
	}
	return s.clientFactory(row.BaseUrl), token, row.ID, nil
}

// DialByConnectionID is Dial, but keyed by the GitLab connection's own ID
// rather than by its owning project's ID. internal/linkedproject uses this
// for webhook registration/repair/delete (issue #18): a linked_gitlab_projects
// row carries gitlab_connection_id but not the app project ID Dial normally
// requires.
func (s *Service) DialByConnectionID(ctx context.Context, ownerID, connectionID uuid.UUID) (gitlab.Client, string, error) {
	row, err := s.q.GetGitlabConnectionByIDForOwner(ctx, db.GetGitlabConnectionByIDForOwnerParams{
		ID:          connectionID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("gitlabconn: dial by connection: %w", err)
	}
	token, err := s.cipher.Decrypt(row.EncryptedToken)
	if err != nil {
		return nil, "", fmt.Errorf("gitlabconn: dial by connection: %w", err)
	}
	return s.clientFactory(row.BaseUrl), token, nil
}

// authorizeOwner requires ownerID to hold project.RoleOwner on projectID.
// Every HTTP-facing method here (Save/Get/Test/Delete) calls this first,
// since the connection carries a credential and stays owner-only even
// though a member may use it indirectly via internal/linkedproject's
// Dial/DialByConnectionID calls (see gitlab_connections.sql).
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
		return fmt.Errorf("gitlabconn: authorize: %w", err)
	}
}

// getRow fetches the raw database row for projectID. It only requires
// ownerID to have *some* project_members row (any role), not specifically
// project.RoleOwner — see gitlab_connections.sql's GetGitlabConnectionForOwner
// comment for why: Dial reuses this for a member-level caller whose
// authorization was already checked by internal/linkedproject.
func (s *Service) getRow(ctx context.Context, ownerID, projectID uuid.UUID) (db.GitlabConnection, error) {
	row, err := s.q.GetGitlabConnectionForOwner(ctx, db.GetGitlabConnectionForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.GitlabConnection{}, ErrNotFound
		}
		return db.GitlabConnection{}, fmt.Errorf("gitlabconn: get: %w", err)
	}
	return row, nil
}
