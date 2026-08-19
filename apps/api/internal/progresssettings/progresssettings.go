// Package progresssettings manages a project's progress-sync-on-close
// opt-in (issue #202): whether closing a task's linked GitLab issue also
// moves the task's progress to 'done' (internal/progresssync does the
// actual write, on the sync path). This package is only the owner-facing
// CRUD side, the same split internal/notification has between its Service
// and the digest Worker.
package progresssettings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sentinel errors returned by Service. internal/http maps these to status
// codes.
var (
	ErrNotFound  = errors.New("progresssettings: not found")
	ErrForbidden = errors.New("progresssettings: forbidden")
)

// Settings is a project's progress-sync-on-close configuration.
type Settings struct {
	ProjectID uuid.UUID `json:"projectId"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Service manages a project's progress-sync settings.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a progresssettings Service.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

func fromRow(row db.ProgressSyncSetting) Settings {
	return Settings{
		ProjectID: row.ProjectID,
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

// Save validates and upserts projectID's progress-sync settings.
func (s *Service) Save(ctx context.Context, ownerID, projectID uuid.UUID, enabled bool) (Settings, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Settings{}, err
	}
	row, err := s.q.UpsertProgressSyncSettings(ctx, db.UpsertProgressSyncSettingsParams{
		ProjectID: projectID,
		Enabled:   enabled,
	})
	if err != nil {
		return Settings{}, fmt.Errorf("progresssettings: save: %w", err)
	}
	return fromRow(row), nil
}

// Get returns projectID's progress-sync settings, or their unconfigured
// default (disabled) if never saved — settings conceptually always exist,
// just possibly unset, the same as notification.Service.Get.
func (s *Service) Get(ctx context.Context, ownerID, projectID uuid.UUID) (Settings, error) {
	if err := s.authorizeOwner(ctx, ownerID, projectID); err != nil {
		return Settings{}, err
	}
	row, err := s.q.GetProgressSyncSettingsForOwner(ctx, db.GetProgressSyncSettingsForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Settings{ProjectID: projectID}, nil
		}
		return Settings{}, fmt.Errorf("progresssettings: get: %w", err)
	}
	return fromRow(row), nil
}

// authorizeOwner requires ownerID to hold project.RoleOwner on projectID:
// enabling this setting changes how tasks are written from an inbound
// GitLab event, the same reasoning notification.Service applies to its
// outbound webhook_url.
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
		return fmt.Errorf("progresssettings: authorize: %w", err)
	}
}
