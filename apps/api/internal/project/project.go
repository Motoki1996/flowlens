// Package project contains the project domain model and the service that
// creates and manages a user's app-level projects. It keeps database types
// (pgtype) from leaking into HTTP responses.
package project

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/database/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
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
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FromDB maps a database row to the domain model.
func FromDB(row db.Project) Project {
	return Project{
		ID:          hex.EncodeToString(row.ID.Bytes[:]),
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
func (s *Service) Create(ctx context.Context, ownerID pgtype.UUID, name, description string) (db.Project, error) {
	normalized, err := normalizeName(name)
	if err != nil {
		return db.Project{}, err
	}
	row, err := s.q.CreateProject(ctx, db.CreateProjectParams{
		OwnerUserID: ownerID,
		Name:        normalized,
		Description: description,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.Project{}, ErrNameTaken
		}
		return db.Project{}, fmt.Errorf("project: create: %w", err)
	}
	return row, nil
}

// List returns every project owned by ownerID.
func (s *Service) List(ctx context.Context, ownerID pgtype.UUID) ([]db.Project, error) {
	rows, err := s.q.ListProjectsByOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	return rows, nil
}

// Get returns the project by ID, scoped to ownerID. It returns ErrNotFound
// both when the project does not exist and when it belongs to another user.
func (s *Service) Get(ctx context.Context, ownerID, projectID pgtype.UUID) (db.Project, error) {
	row, err := s.q.GetProjectByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Project{}, ErrNotFound
		}
		return db.Project{}, fmt.Errorf("project: get: %w", err)
	}
	if row.OwnerUserID != ownerID {
		return db.Project{}, ErrNotFound
	}
	return row, nil
}

// Update overwrites name and description. Callers are responsible for
// verifying ownership first; Update does not check it.
func (s *Service) Update(ctx context.Context, projectID pgtype.UUID, name, description string) (db.Project, error) {
	normalized, err := normalizeName(name)
	if err != nil {
		return db.Project{}, err
	}
	row, err := s.q.UpdateProject(ctx, db.UpdateProjectParams{
		ID:          projectID,
		Name:        normalized,
		Description: description,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.Project{}, ErrNameTaken
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Project{}, ErrNotFound
		}
		return db.Project{}, fmt.Errorf("project: update: %w", err)
	}
	return row, nil
}

// Delete removes the project. Callers are responsible for verifying
// ownership first; Delete does not check it.
func (s *Service) Delete(ctx context.Context, projectID pgtype.UUID) error {
	if err := s.q.DeleteProject(ctx, projectID); err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}
	return nil
}
