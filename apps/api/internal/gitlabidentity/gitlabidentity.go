// Package gitlabidentity contains the domain model and service that map a
// FlowLens user to their GitLab user ID/username on one GitLab CE instance
// (issue #102): user_gitlab_identities, keyed by (user_id, gitlab_base_url)
// since GitLab CE is self-hosted and a team may have more than one instance.
// It carries no access token — that is a distinct, still-unbuilt feature
// (see ADR-0008) — only the identifiers internal/task's ?assignee=me filter
// needs to match against tasks.assignee_gitlab_user_id.
//
// Service accepts and returns only types declared here; database row types
// never cross the package boundary, the same convention internal/user
// follows.
package gitlabidentity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
)

// ErrInvalidBaseURL is returned when a GitLab base URL is empty.
var ErrInvalidBaseURL = errors.New("gitlabidentity: base URL must not be empty")

// Identity is the API-facing representation of one registered GitLab
// identity.
type Identity struct {
	ID             uuid.UUID `json:"id"`
	GitlabBaseURL  string    `json:"gitlabBaseUrl"`
	GitlabUserID   int64     `json:"gitlabUserId"`
	GitlabUsername string    `json:"gitlabUsername"`
}

func fromRow(row db.UserGitlabIdentity) Identity {
	return Identity{
		ID:             row.ID,
		GitlabBaseURL:  row.GitlabBaseUrl,
		GitlabUserID:   row.GitlabUserID,
		GitlabUsername: row.GitlabUsername,
	}
}

// Service manages GitLab identities.
type Service struct {
	q db.Querier
}

// NewService constructs a gitlabidentity Service.
func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

// UpsertInput holds the fields required to register or replace one identity.
type UpsertInput struct {
	GitlabBaseURL  string
	GitlabUserID   int64
	GitlabUsername string
}

// Upsert registers userID's GitLab identity for one base URL, replacing any
// previously registered identity for that same (userID, baseURL) pair.
func (s *Service) Upsert(ctx context.Context, userID uuid.UUID, in UpsertInput) (Identity, error) {
	baseURL := strings.TrimSpace(in.GitlabBaseURL)
	if baseURL == "" {
		return Identity{}, ErrInvalidBaseURL
	}
	row, err := s.q.UpsertUserGitlabIdentity(ctx, db.UpsertUserGitlabIdentityParams{
		UserID:         userID,
		GitlabBaseUrl:  baseURL,
		GitlabUserID:   in.GitlabUserID,
		GitlabUsername: in.GitlabUsername,
	})
	if err != nil {
		return Identity{}, fmt.Errorf("gitlabidentity: upsert: %w", err)
	}
	return fromRow(row), nil
}

// ListForUser returns every GitLab identity userID has registered, one per
// base URL, ordered by base URL.
func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID) ([]Identity, error) {
	rows, err := s.q.ListUserGitlabIdentitiesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("gitlabidentity: list: %w", err)
	}
	out := make([]Identity, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}
