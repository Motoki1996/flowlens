// Package backlog contains the backlog domain model and the service that
// creates and manages the backlogs inside a project.
//
// Backlogs carry no owner column of their own: every method takes the
// acting user's ID and the parent project, and ownership is always checked
// through the project (directly via project.Service.Get, or in the SQL
// WHERE/JOIN for the single-backlog queries). A backlog belonging to
// another user's project is indistinguishable from a missing one
// (ErrNotFound), matching internal/project.
package backlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is returned both when a backlog/project does not exist
// and when it belongs to another user.
var (
	ErrInvalidName = errors.New("backlog: name must be 1-100 characters")
	ErrNotFound    = errors.New("backlog: not found")
)

// Backlog is the API-facing representation of a project backlog.
type Backlog struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"projectId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Position    int32     `json:"position"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.Backlog) Backlog {
	return Backlog{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		Name:        row.Name,
		Description: row.Description,
		Position:    row.Position,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

// Service manages backlogs inside projects owned by a single user.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a backlog Service. projects is used to verify
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

// Create validates name and creates a backlog at the end of projectID's
// backlog order. It returns ErrNotFound if projectID does not exist or
// belongs to another user.
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, name, description string) (Backlog, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return Backlog{}, ErrNotFound
		}
		return Backlog{}, fmt.Errorf("backlog: create: %w", err)
	}

	normalized, err := normalizeName(name)
	if err != nil {
		return Backlog{}, err
	}
	row, err := s.q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID:   projectID,
		Name:        normalized,
		Description: description,
	})
	if err != nil {
		return Backlog{}, fmt.Errorf("backlog: create: %w", err)
	}
	return fromRow(row), nil
}

// List returns every backlog in projectID, ordered by position. It returns
// ErrNotFound if projectID does not exist or belongs to another user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID) ([]Backlog, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("backlog: list: %w", err)
	}

	rows, err := s.q.ListBacklogsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("backlog: list: %w", err)
	}
	out := make([]Backlog, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Get returns the backlog by ID, scoped through its project's owner. It
// returns ErrNotFound both when the backlog does not exist and when its
// project belongs to another user.
func (s *Service) Get(ctx context.Context, ownerID, backlogID uuid.UUID) (Backlog, error) {
	row, err := s.q.GetBacklogForOwner(ctx, db.GetBacklogForOwnerParams{
		ID:          backlogID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backlog{}, ErrNotFound
		}
		return Backlog{}, fmt.Errorf("backlog: get: %w", err)
	}
	return fromRow(row), nil
}

// Update overwrites name, description and position. Ownership is enforced
// by the query, so a non-owner gets ErrNotFound and nothing is written.
func (s *Service) Update(ctx context.Context, ownerID, backlogID uuid.UUID, name, description string, position int32) (Backlog, error) {
	normalized, err := normalizeName(name)
	if err != nil {
		return Backlog{}, err
	}
	row, err := s.q.UpdateBacklogForOwner(ctx, db.UpdateBacklogForOwnerParams{
		ID:          backlogID,
		OwnerUserID: ownerID,
		Name:        normalized,
		Description: description,
		Position:    position,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backlog{}, ErrNotFound
		}
		return Backlog{}, fmt.Errorf("backlog: update: %w", err)
	}
	return fromRow(row), nil
}

// Delete removes the backlog. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted. Tasks in the backlog
// are not deleted: the schema's ON DELETE SET NULL drops them to
// unfiled (backlog_id = NULL).
func (s *Service) Delete(ctx context.Context, ownerID, backlogID uuid.UUID) error {
	affected, err := s.q.DeleteBacklogForOwner(ctx, db.DeleteBacklogForOwnerParams{
		ID:          backlogID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return fmt.Errorf("backlog: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
