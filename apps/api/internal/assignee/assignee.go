// Package assignee holds the two lookups a task's and a backlog's FlowLens
// assignee both need (the 000031 migration): validating that the assignee is
// actually a member of the project, and resolving a batch of assignee IDs to
// the names a response renders.
//
// It lives in its own package for the same reason internal/optional does:
// internal/task imports internal/backlog, so backlog cannot import task back
// to share this.
package assignee

import (
	"context"
	"errors"
	"fmt"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotMember is returned when the requested assignee is not a member of the
// project the task or backlog belongs to. Assigning work to someone who
// cannot see the project is never what the caller meant, and project_members
// is the only set the assignee picker offers, so this is a validation error
// (422) rather than a silent no-op.
var ErrNotMember = errors.New("assignee: not a member of the project")

// Name is a user's rendered identity, resolved from users. DisplayName is ""
// for an account that never set one — callers fall back to Username.
type Name struct {
	Username    string
	DisplayName string
}

// GitlabIdentity is the GitLab user an assignment mirrors onto, when the
// assignee has an identity registered for the project's GitLab instance.
// Found is false when they don't (or the project has no GitLab connection at
// all), which is an ordinary FlowLens-only assignment, not an error.
type GitlabIdentity struct {
	UserID   int64
	Username string
	Found    bool
}

// ValidateMember reports ErrNotMember unless userID is a member of projectID.
func ValidateMember(ctx context.Context, q db.Querier, projectID, userID uuid.UUID) error {
	if _, err := q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{
		ProjectID: projectID,
		UserID:    userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotMember
		}
		return fmt.Errorf("assignee: validate member: %w", err)
	}
	return nil
}

// ResolveGitlab looks up the GitLab identity userID has registered for
// projectID's GitLab connection — the one-way bridge that puts a FlowLens
// assignment onto the GitLab issue. A missing row is not an error; see
// GitlabIdentity.Found.
func ResolveGitlab(ctx context.Context, q db.Querier, projectID, userID uuid.UUID) (GitlabIdentity, error) {
	row, err := q.GetProjectAssigneeGitlabIdentity(ctx, db.GetProjectAssigneeGitlabIdentityParams{
		ProjectID: projectID,
		UserID:    userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GitlabIdentity{}, nil
		}
		return GitlabIdentity{}, fmt.Errorf("assignee: resolve gitlab identity: %w", err)
	}
	return GitlabIdentity{UserID: row.GitlabUserID, Username: row.GitlabUsername, Found: true}, nil
}

// Names resolves ids to their usernames in one query, de-duplicating and
// skipping nils so callers can pass a whole list's worth of assignee pointers
// without filtering first. An id with no row (a user deleted between the two
// queries) is simply absent from the map.
func Names(ctx context.Context, q db.Querier, ids []*uuid.UUID) (map[uuid.UUID]Name, error) {
	unique := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == nil {
			continue
		}
		if _, dup := seen[*id]; dup {
			continue
		}
		seen[*id] = struct{}{}
		unique = append(unique, *id)
	}
	if len(unique) == 0 {
		return map[uuid.UUID]Name{}, nil
	}
	rows, err := q.ListUsersByIDs(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("assignee: resolve names: %w", err)
	}
	out := make(map[uuid.UUID]Name, len(rows))
	for _, row := range rows {
		out[row.ID] = Name{Username: row.Username, DisplayName: row.DisplayName}
	}
	return out, nil
}
