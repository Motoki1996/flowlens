// Package syncjob is the troubleshooting read/retry side of the outbox
// worker's sync_jobs table (issue #97): a job that exhausted internal/sync's
// retry budget is marked 'failed' and otherwise sits invisible in the
// database — this package lists a project's failed jobs and lets one be
// retried directly by ID, the project-scoped counterpart to
// internal/task.Service.RetrySync (which retries by task instead).
package syncjob

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

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes.
var (
	// ErrNotFound covers an unknown projectID/jobID and one that exists but
	// belongs to another user's project — the two are never distinguished,
	// the same as every other project-scoped resource.
	ErrNotFound = errors.New("syncjob: not found")
	// ErrForbidden means the caller has a project_members row but with too
	// low a role for the action (issue #99).
	ErrForbidden = errors.New("syncjob: forbidden")
	// ErrNotFailed is returned by Retry when jobID is not currently 'failed'
	// (already pending/running/succeeded, or a concurrent retry already
	// moved it).
	ErrNotFailed = errors.New("syncjob: job is not in a failed state")
)

// Job is the API-facing representation of one sync_jobs row.
type Job struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID uuid.UUID  `json:"projectId"`
	TaskID    *uuid.UUID `json:"taskId,omitempty"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	Attempts  int32      `json:"attempts"`
	LastError string     `json:"lastError"`
	RunAfter  time.Time  `json:"runAfter"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func fromRow(row db.SyncJob) Job {
	j := Job{
		ID:        row.ID,
		ProjectID: row.ProjectID,
		Kind:      row.Kind,
		Status:    row.Status,
		Attempts:  row.Attempts,
		LastError: row.LastError,
		RunAfter:  row.RunAfter.Time,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.TaskID.Valid {
		id := uuid.UUID(row.TaskID.Bytes)
		j.TaskID = &id
	}
	return j
}

// Service lists and retries a project's failed sync jobs.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a Service.
func NewService(q db.Querier, projects *project.Service) *Service {
	return &Service{q: q, projects: projects}
}

// authorize requires ownerID to hold at least min on projectID, mapping
// project.ErrNotFound/ErrForbidden to this package's own sentinels.
func (s *Service) authorize(ctx context.Context, ownerID, projectID uuid.UUID, min project.Role) error {
	err := s.projects.Authorize(ctx, ownerID, projectID, min)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, project.ErrForbidden):
		return ErrForbidden
	default:
		return fmt.Errorf("syncjob: authorize: %w", err)
	}
}

// ListFailed returns every permanently-failed sync job for projectID, newest
// first, scoped to ownerID. It returns ErrNotFound if projectID does not
// exist or belongs to another user.
func (s *Service) ListFailed(ctx context.Context, ownerID, projectID uuid.UUID) ([]Job, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.q.ListFailedSyncJobsByProjectForOwner(ctx, db.ListFailedSyncJobsByProjectForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return nil, fmt.Errorf("syncjob: list failed: %w", err)
	}
	out := make([]Job, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Retry resets a failed job back to pending with a fresh attempt budget, so
// internal/sync's Worker picks it up again, scoped to ownerID through the
// job's project. It returns ErrNotFound if jobID does not exist or its
// project belongs to another user, and ErrNotFailed if jobID exists but is
// not currently 'failed'.
func (s *Service) Retry(ctx context.Context, ownerID, jobID uuid.UUID) (Job, error) {
	existing, err := s.q.GetSyncJobForOwner(ctx, db.GetSyncJobForOwnerParams{ID: jobID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, fmt.Errorf("syncjob: retry: %w", err)
	}
	if err := s.authorize(ctx, ownerID, existing.ProjectID, project.RoleMember); err != nil {
		return Job{}, err
	}
	if existing.Status != "failed" {
		return Job{}, ErrNotFailed
	}

	row, err := s.q.RetrySyncJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost a race against a concurrent retry (or the worker) that
			// already moved the job out of 'failed' between the read above
			// and this write.
			return Job{}, ErrNotFailed
		}
		return Job{}, fmt.Errorf("syncjob: retry: %w", err)
	}
	return fromRow(row), nil
}
