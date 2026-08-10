// Package project contains the project domain model and the service that
// creates and manages a user's app-level projects.
//
// Two rules hold for every method here and for every project-scoped service
// added later:
//
//   - Service accepts and returns only types declared here (Project,
//     uuid.UUID). Database row types never cross the package boundary.
//   - Every method takes the acting user's ID and enforces access in the SQL
//     WHERE clause (a project_members row, not owner_user_id equality — see
//     docs/decisions/0010-why-project-membership.md and Role/Authorize
//     below). Callers never perform their own access check, and a caller
//     with no membership row is indistinguishable from a missing project
//     (ErrNotFound); one with an insufficient role gets ErrForbidden.
package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound means the caller has no project_members row at all for
// projectID (the project doesn't exist, or the caller was never added to
// it — the two are indistinguishable on purpose, so existence is never
// leaked). ErrForbidden means the caller does have a membership row, but its
// role is below what the action requires.
var (
	ErrInvalidName = errors.New("project: name must be 1-100 characters")
	ErrNameTaken   = errors.New("project: name already taken")
	ErrNotFound    = errors.New("project: not found")
	ErrForbidden   = errors.New("project: forbidden")
)

// Role is a caller's project_members role, ordered so comparison operators
// express "at least as privileged as": RoleViewer < RoleMember < RoleOwner.
// RoleNone has no project_members representation — it means the caller has
// no membership row for the project at all.
type Role int

const (
	RoleNone Role = iota
	RoleViewer
	RoleMember
	RoleOwner
)

// String returns the project_members.role TEXT value for r, or "" for
// RoleNone, which has no DB representation.
func (r Role) String() string {
	switch r {
	case RoleOwner:
		return "owner"
	case RoleMember:
		return "member"
	case RoleViewer:
		return "viewer"
	default:
		return ""
	}
}

// roleFromDB converts a project_members.role TEXT value to a Role. An
// unrecognized value (which the DB CHECK constraint should prevent) maps to
// RoleNone, the safest failure mode for an authorization check.
func roleFromDB(s string) Role {
	switch s {
	case "owner":
		return RoleOwner
	case "member":
		return RoleMember
	case "viewer":
		return RoleViewer
	default:
		return RoleNone
	}
}

// Project is the API-facing representation of a FlowLens project.
//
// FailedSyncTaskCount is zero unless a caller explicitly sets it from
// Service.FailedSyncTaskCount — only the single-project HTTP handler does,
// for the single view's warning banner (docs/plans/issue-sync.md). Get and
// List never populate it themselves; see Get's doc comment for why.
// ListFailedSync is the exception: it populates FailedSyncTaskCount as part
// of its own query, since every project it returns has a non-zero count by
// construction.
type Project struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	FailedSyncTaskCount int64     `json:"failedSyncTaskCount"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.Project) Project {
	return Project{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

// Service manages projects owned by a single user.
type Service struct {
	q db.Querier
}

// NewService constructs a project Service.
func NewService(q db.Querier) *Service {
	return &Service{q: q}
}

// normalizeName trims raw and enforces the 1-100 character rule.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ErrInvalidName
	}
	return name, nil
}

// Create validates name and creates a project owned by ownerID. ownerID also
// gets an 'owner'-role project_members row in the same call, so Role/
// Authorize work for the creator immediately — the same invariant migration
// 000012 backfilled for every project that existed before project_members
// was added (docs/decisions/0010-why-project-membership.md).
func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name, description string) (Project, error) {
	normalized, err := normalizeName(name)
	if err != nil {
		return Project{}, err
	}
	row, err := s.q.CreateProject(ctx, db.CreateProjectParams{
		OwnerUserID: ownerID,
		Name:        normalized,
		Description: description,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Project{}, ErrNameTaken
		}
		return Project{}, fmt.Errorf("project: create: %w", err)
	}
	if _, err := s.q.AddProjectMember(ctx, db.AddProjectMemberParams{
		ProjectID: row.ID,
		UserID:    ownerID,
		Role:      RoleOwner.String(),
	}); err != nil {
		return Project{}, fmt.Errorf("project: create: add owner membership: %w", err)
	}
	return fromRow(row), nil
}

// Role reports userID's project_members role for projectID. It returns
// RoleNone, not an error, when there is no membership row at all — no
// membership is a valid state to be in, not a failure.
func (s *Service) Role(ctx context.Context, userID, projectID uuid.UUID) (Role, error) {
	role, err := s.q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{
		ProjectID: projectID,
		UserID:    userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RoleNone, nil
		}
		return RoleNone, fmt.Errorf("project: role: %w", err)
	}
	return roleFromDB(role), nil
}

// Authorize is the primitive every project-scoped service uses to gate an
// action that doesn't need the Project value back (replacing what used to be
// a call to Get whose return value was discarded). It returns ErrNotFound
// when userID has no membership row for projectID at all, ErrForbidden when
// it has one but below min, and nil when userID may proceed.
func (s *Service) Authorize(ctx context.Context, userID, projectID uuid.UUID, min Role) error {
	role, err := s.Role(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if role == RoleNone {
		return ErrNotFound
	}
	if role < min {
		return ErrForbidden
	}
	return nil
}

// List returns every project ownerID is a member of, any role.
func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]Project, error) {
	rows, err := s.q.ListProjectsByMember(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	out := make([]Project, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Get returns the project by ID. It returns ErrNotFound both when the
// project does not exist and when the caller has no project_members row for
// it — Get is viewer-minimum, the lowest tier, so this covers "not a member
// at all"; a member below Get's own no-op minimum can't happen.
//
// Get never populates FailedSyncTaskCount itself: Get is also used
// internally, by this package's own callers and others (internal/task,
// internal/backlog, internal/gitlabconn, internal/linkedproject), purely as
// a read-access check whose Project value is discarded. Running the count
// query on every one of those would be silent, unwanted overhead. Only the
// single-project HTTP handler needs the count, so it calls
// FailedSyncTaskCount itself after Get succeeds.
func (s *Service) Get(ctx context.Context, ownerID, projectID uuid.UUID) (Project, error) {
	row, err := s.q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{
		ID:          projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, fmt.Errorf("project: get: %w", err)
	}
	return fromRow(row), nil
}

// FailedSyncTaskCount returns how many of projectID's tasks currently have a
// failed GitLab sync (docs/plans/issue-sync.md), for the project single
// view's warning banner. It returns 0, not ErrNotFound, for a project that
// doesn't exist or belongs to another user — callers are expected to have
// already confirmed the project via Get.
func (s *Service) FailedSyncTaskCount(ctx context.Context, ownerID, projectID uuid.UUID) (int64, error) {
	count, err := s.q.CountFailedSyncTasksByProjectForOwner(ctx, db.CountFailedSyncTasksByProjectForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return 0, fmt.Errorf("project: count failed sync tasks: %w", err)
	}
	return count, nil
}

// ListFailedSync returns every project owned by ownerID that currently has
// at least one task with a failed GitLab sync, ordered most-recently-updated
// first, for the dashboard's "sync failures" section. Unlike List, the
// returned Projects have FailedSyncTaskCount populated — every one of them
// is non-zero by construction, since a zero-count project never has a
// failed-sync task to match on.
func (s *Service) ListFailedSync(ctx context.Context, ownerID uuid.UUID) ([]Project, error) {
	rows, err := s.q.ListFailedSyncProjectsByMember(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("project: list failed sync: %w", err)
	}
	out := make([]Project, len(rows))
	for i, row := range rows {
		out[i] = Project{
			ID:                  row.ID,
			Name:                row.Name,
			Description:         row.Description,
			CreatedAt:           row.CreatedAt.Time,
			UpdatedAt:           row.UpdatedAt.Time,
			FailedSyncTaskCount: row.FailedSyncTaskCount,
		}
	}
	return out, nil
}

// Update overwrites name and description. It is member-minimum (not
// explicitly named owner-only by issue #99, so it follows the "writes are
// member-tier by default" rule). Authorize distinguishes ErrForbidden (a
// viewer tried to write) from ErrNotFound (no membership row at all) before
// anything is written.
func (s *Service) Update(ctx context.Context, ownerID, projectID uuid.UUID, name, description string) (Project, error) {
	if err := s.Authorize(ctx, ownerID, projectID, RoleMember); err != nil {
		return Project{}, err
	}
	normalized, err := normalizeName(name)
	if err != nil {
		return Project{}, err
	}
	row, err := s.q.UpdateProjectForOwner(ctx, db.UpdateProjectForOwnerParams{
		ID:          projectID,
		OwnerUserID: ownerID,
		Name:        normalized,
		Description: description,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Project{}, ErrNameTaken
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, fmt.Errorf("project: update: %w", err)
	}
	return fromRow(row), nil
}

// Delete removes the project. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted.
func (s *Service) Delete(ctx context.Context, ownerID, projectID uuid.UUID) error {
	affected, err := s.q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{
		ID:          projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation, which for projects can only be (owner_user_id, name).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
