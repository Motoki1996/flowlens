// Package project contains the project domain model and the service that
// creates and manages a user's app-level projects.
//
// Two rules hold for every method here and for every project-scoped service
// added later:
//
//   - Service accepts and returns only types declared here (Project,
//     uuid.UUID). Database row types never cross the package boundary.
//   - Every method takes the acting user's ID and enforces ownership in the
//     SQL WHERE clause. Callers never perform their own ownership check, and
//     a non-owner is indistinguishable from a missing project (ErrNotFound).
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
// codes. ErrNotFound is also returned when a project exists but belongs to
// another user, so callers never leak existence to non-owners.
var (
	ErrInvalidName = errors.New("project: name must be 1-100 characters")
	ErrNameTaken   = errors.New("project: name already taken")
	ErrNotFound    = errors.New("project: not found")
)

// Project is the API-facing representation of a FlowLens project.
//
// FailedSyncTaskCount is zero unless a caller explicitly sets it from
// Service.FailedSyncTaskCount — only the single-project HTTP handler does,
// for the single view's warning banner (docs/plans/issue-sync.md). Get and
// List never populate it themselves; see Get's doc comment for why.
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

// Create validates name and creates a project owned by ownerID.
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
	return fromRow(row), nil
}

// List returns every project owned by ownerID.
func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]Project, error) {
	rows, err := s.q.ListProjectsByOwner(ctx, ownerID)
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
// project does not exist and when it belongs to another user.
//
// Get never populates FailedSyncTaskCount itself: Get is also used
// internally, by this package's own callers and others (internal/task,
// internal/backlog, internal/gitlabconn, internal/linkedproject), purely as
// an ownership check whose Project value is discarded. Running the count
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

// Update overwrites name and description. Ownership is enforced by the
// query, so a non-owner gets ErrNotFound and nothing is written.
func (s *Service) Update(ctx context.Context, ownerID, projectID uuid.UUID, name, description string) (Project, error) {
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
