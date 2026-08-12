// Package projectmember manages a project's project_members rows: inviting
// an existing user, listing members, changing a member's role, and removing
// one — the first thing to actually write to project_members since it was
// added by docs/decisions/0010-why-project-membership.md. Every method is
// owner-minimum (issue #100): managing who can access a project is a
// project-management action, not a day-to-day member one, the same rule
// internal/apitoken already follows for issuing credentials.
package projectmember

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/user"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound covers both "no such project" (no membership row for
// the caller at all) and "no such member" (target user has no
// project_members row for this project) — the two are never distinguished,
// mirroring how project.ErrNotFound already treats project existence.
var (
	ErrNotFound      = errors.New("projectmember: not found")
	ErrForbidden     = errors.New("projectmember: forbidden")
	ErrUserNotFound  = errors.New("projectmember: user not found")
	ErrAlreadyMember = errors.New("projectmember: already a member")
	ErrInvalidRole   = errors.New("projectmember: role must be owner, member, or viewer")
	ErrLastOwner     = errors.New("projectmember: cannot demote or remove the project's owner")
	ErrSelfRole      = errors.New("projectmember: cannot change your own role")
	ErrSelfRemove    = errors.New("projectmember: cannot remove yourself from the project")
)

// Member is the API-facing representation of a project_members row, joined
// with the user it belongs to. Email is deliberately absent: issue #100
// requires the response never reveal it, to avoid using this endpoint to
// enumerate registered email addresses.
//
// IsProjectOwner marks the row belonging to the project's single designated
// owner (projects.owner_user_id), which Role alone cannot identify — any
// number of members may hold the "owner" role. It is what lets a client hide
// the demote/remove controls that would only ever come back as ErrLastOwner
// (issue #139).
type Member struct {
	UserID         uuid.UUID `json:"userId"`
	Username       string    `json:"username"`
	DisplayName    string    `json:"displayName"`
	Role           string    `json:"role"`
	IsProjectOwner bool      `json:"isProjectOwner"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Service manages project_members rows for projects owned by a single user.
type Service struct {
	q        db.Querier
	projects *project.Service
	users    *user.Service
}

// NewService constructs a projectmember Service. projects verifies the
// caller's role before any operation; users resolves the username/email a
// caller invites to the user it belongs to.
func NewService(q db.Querier, projects *project.Service, users *user.Service) *Service {
	return &Service{q: q, projects: projects, users: users}
}

// authorize requires callerID to hold at least project.RoleOwner on
// projectID, mapping project.ErrNotFound/ErrForbidden to this package's own
// sentinels.
func (s *Service) authorize(ctx context.Context, callerID, projectID uuid.UUID) error {
	err := s.projects.Authorize(ctx, callerID, projectID, project.RoleOwner)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return ErrForbidden
	default:
		return fmt.Errorf("projectmember: authorize: %w", err)
	}
}

// normalizeRole validates raw against the owner/member/viewer vocabulary.
func normalizeRole(raw string) (string, error) {
	switch raw {
	case project.RoleOwner.String(), project.RoleMember.String(), project.RoleViewer.String():
		return raw, nil
	default:
		return "", ErrInvalidRole
	}
}

// List returns every member of projectID, oldest first.
func (s *Service) List(ctx context.Context, callerID, projectID uuid.UUID) ([]Member, error) {
	if err := s.authorize(ctx, callerID, projectID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListProjectMembersWithUser(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("projectmember: list: %w", err)
	}
	ownerUserID, err := s.projects.OwnerUserID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("projectmember: list: %w", err)
	}
	out := make([]Member, len(rows))
	for i, row := range rows {
		out[i] = Member{
			UserID:         row.UserID,
			Username:       row.Username,
			DisplayName:    row.DisplayName,
			Role:           row.Role,
			IsProjectOwner: row.UserID == ownerUserID,
			CreatedAt:      row.CreatedAt.Time,
		}
	}
	return out, nil
}

// Add invites an existing user, resolved by username or email, to projectID
// with the given role. It returns ErrUserNotFound if no user matches
// identifier — the response carries no email, so this is the only signal
// about the target's existence a caller ever gets — and ErrAlreadyMember if
// the resolved user already has a project_members row for projectID.
func (s *Service) Add(ctx context.Context, callerID, projectID uuid.UUID, identifier, role string) (Member, error) {
	if err := s.authorize(ctx, callerID, projectID); err != nil {
		return Member{}, err
	}
	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return Member{}, err
	}

	target, err := s.users.ByUsernameOrEmail(ctx, identifier)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return Member{}, ErrUserNotFound
		}
		return Member{}, fmt.Errorf("projectmember: add: lookup user: %w", err)
	}

	row, err := s.q.AddProjectMember(ctx, db.AddProjectMemberParams{
		ProjectID: projectID,
		UserID:    target.ID,
		Role:      normalizedRole,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Member{}, ErrAlreadyMember
		}
		return Member{}, fmt.Errorf("projectmember: add: %w", err)
	}
	// IsProjectOwner is always false here: the designated owner is given a
	// project_members row when the project is created (project.Service.Create),
	// so inviting them again can only ever return ErrAlreadyMember above.
	return Member{
		UserID:      target.ID,
		Username:    target.Username,
		DisplayName: target.DisplayName,
		Role:        row.Role,
		CreatedAt:   row.CreatedAt.Time,
	}, nil
}

// UpdateRole changes a member's role. It returns ErrSelfRole if callerID and
// userID are the same, and ErrLastOwner if userID is projectID's designated
// owner (project.Service.OwnerUserID) and role is not itself "owner" — see
// docs/decisions/0010-why-project-membership.md for why that single column,
// not a COUNT(*) over project_members, is the source of truth here.
func (s *Service) UpdateRole(ctx context.Context, callerID, projectID, userID uuid.UUID, role string) (Member, error) {
	if err := s.authorize(ctx, callerID, projectID); err != nil {
		return Member{}, err
	}
	if userID == callerID {
		return Member{}, ErrSelfRole
	}
	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return Member{}, err
	}

	ownerUserID, err := s.projects.OwnerUserID(ctx, projectID)
	if err != nil {
		return Member{}, fmt.Errorf("projectmember: update role: %w", err)
	}
	if userID == ownerUserID && normalizedRole != project.RoleOwner.String() {
		return Member{}, ErrLastOwner
	}

	row, err := s.q.UpdateProjectMemberRole(ctx, db.UpdateProjectMemberRoleParams{
		ProjectID: projectID,
		UserID:    userID,
		Role:      normalizedRole,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Member{}, ErrNotFound
		}
		return Member{}, fmt.Errorf("projectmember: update role: %w", err)
	}

	target, err := s.users.ByID(ctx, userID)
	if err != nil {
		return Member{}, fmt.Errorf("projectmember: update role: %w", err)
	}
	return Member{
		UserID:         userID,
		Username:       target.Username,
		DisplayName:    target.DisplayName,
		Role:           row.Role,
		IsProjectOwner: userID == ownerUserID,
		CreatedAt:      row.CreatedAt.Time,
	}, nil
}

// Remove deletes a member's project_members row. It returns ErrSelfRemove if
// callerID and userID are the same — this endpoint manages *other* people's
// access, and a co-owner removing themselves would silently lock themselves
// out of a project they cannot get back into (issue #139); leaving a project
// on purpose would be its own action. It returns ErrLastOwner if userID is
// projectID's designated owner (see UpdateRole's doc comment) — removing them
// would leave the project with no owner_user_id row in project_members at
// all.
func (s *Service) Remove(ctx context.Context, callerID, projectID, userID uuid.UUID) error {
	if err := s.authorize(ctx, callerID, projectID); err != nil {
		return err
	}
	if userID == callerID {
		return ErrSelfRemove
	}

	ownerUserID, err := s.projects.OwnerUserID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("projectmember: remove: %w", err)
	}
	if userID == ownerUserID {
		return ErrLastOwner
	}

	affected, err := s.q.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{
		ProjectID: projectID,
		UserID:    userID,
	})
	if err != nil {
		return fmt.Errorf("projectmember: remove: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation, which for project_members can only be its (project_id,
// user_id) primary key.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
