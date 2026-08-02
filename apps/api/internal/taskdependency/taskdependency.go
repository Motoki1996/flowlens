// Package taskdependency contains the task-dependency domain model and the
// service that manages predecessor/successor relationships between tasks in
// the same project, for the Gantt-chart schedule view (issue #33). A
// dependency means "the predecessor must finish before the successor
// starts"; it is purely app-local, with no GitLab equivalent to sync.
//
// task_dependencies carries no project_id or owner column of its own, the
// same way task_gitlab_links doesn't: both tasks in a dependency always
// belong to the same project (enforced here, in Create, not by the
// database), so ownership is checked by resolving each task through
// internal/task, which already scopes tasks by their project's owner.
package taskdependency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes.
var (
	ErrNotFound         = errors.New("taskdependency: not found")
	ErrSelfDependency   = errors.New("taskdependency: a task cannot depend on itself")
	ErrTaskNotInProject = errors.New("taskdependency: task belongs to a different project")
	ErrCyclicDependency = errors.New("taskdependency: would create a cyclic dependency")
)

// TaskDependency is the API-facing representation of a predecessor/successor
// relationship between two tasks.
type TaskDependency struct {
	ID                uuid.UUID `json:"id"`
	PredecessorTaskID uuid.UUID `json:"predecessorTaskId"`
	SuccessorTaskID   uuid.UUID `json:"successorTaskId"`
	CreatedAt         time.Time `json:"createdAt"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.TaskDependency) TaskDependency {
	return TaskDependency{
		ID:                row.ID,
		PredecessorTaskID: row.PredecessorTaskID,
		SuccessorTaskID:   row.SuccessorTaskID,
		CreatedAt:         row.CreatedAt.Time,
	}
}

// Service manages task dependencies inside projects owned by a single user.
type Service struct {
	q        db.Querier
	projects *project.Service
	tasks    *task.Service
}

// NewService constructs a taskdependency Service. projects and tasks are
// used to verify project ownership and task membership before any write.
func NewService(q db.Querier, projects *project.Service, tasks *task.Service) *Service {
	return &Service{q: q, projects: projects, tasks: tasks}
}

// validateTaskInProject checks that taskID belongs to ownerID and is inside
// projectID, the same way task.Service.validateBacklog checks a backlog.
func (s *Service) validateTaskInProject(ctx context.Context, ownerID, projectID, taskID uuid.UUID) error {
	t, err := s.tasks.Get(ctx, ownerID, taskID)
	if err != nil {
		if errors.Is(err, task.ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("taskdependency: validate task: %w", err)
	}
	if t.ProjectID != projectID {
		return ErrTaskNotInProject
	}
	return nil
}

// wouldCycle reports whether adding a predecessorID -> successorID edge to
// deps would create a cycle: true when predecessorID is already reachable
// from successorID by following existing edges forward (successor ->
// successor's successors -> ...). If it is, the new edge would close a loop
// back to predecessorID.
func wouldCycle(deps []db.TaskDependency, predecessorID, successorID uuid.UUID) bool {
	if predecessorID == successorID {
		return true
	}
	successorsOf := make(map[uuid.UUID][]uuid.UUID, len(deps))
	for _, d := range deps {
		successorsOf[d.PredecessorTaskID] = append(successorsOf[d.PredecessorTaskID], d.SuccessorTaskID)
	}

	visited := map[uuid.UUID]bool{successorID: true}
	queue := []uuid.UUID{successorID}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == predecessorID {
			return true
		}
		for _, n := range successorsOf[next] {
			if !visited[n] {
				visited[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

// Create validates that predecessorTaskID and successorTaskID both belong to
// projectID and that linking them would not create a cycle, then records the
// dependency. It returns ErrNotFound if projectID or either task does not
// exist or belongs to another user, ErrTaskNotInProject if a task belongs to
// a different project, ErrSelfDependency if the two IDs are equal, and
// ErrCyclicDependency if the new edge would close a loop.
func (s *Service) Create(ctx context.Context, ownerID, projectID, predecessorTaskID, successorTaskID uuid.UUID) (TaskDependency, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return TaskDependency{}, ErrNotFound
		}
		return TaskDependency{}, fmt.Errorf("taskdependency: create: %w", err)
	}
	if predecessorTaskID == successorTaskID {
		return TaskDependency{}, ErrSelfDependency
	}
	if err := s.validateTaskInProject(ctx, ownerID, projectID, predecessorTaskID); err != nil {
		return TaskDependency{}, err
	}
	if err := s.validateTaskInProject(ctx, ownerID, projectID, successorTaskID); err != nil {
		return TaskDependency{}, err
	}

	deps, err := s.q.ListTaskDependenciesByProject(ctx, projectID)
	if err != nil {
		return TaskDependency{}, fmt.Errorf("taskdependency: create: list existing: %w", err)
	}
	if wouldCycle(deps, predecessorTaskID, successorTaskID) {
		return TaskDependency{}, ErrCyclicDependency
	}

	row, err := s.q.CreateTaskDependency(ctx, db.CreateTaskDependencyParams{
		PredecessorTaskID: predecessorTaskID,
		SuccessorTaskID:   successorTaskID,
	})
	if err != nil {
		return TaskDependency{}, fmt.Errorf("taskdependency: create: %w", err)
	}
	return fromRow(row), nil
}

// List returns every dependency between tasks in projectID. It returns
// ErrNotFound if projectID does not exist or belongs to another user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID) ([]TaskDependency, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("taskdependency: list: %w", err)
	}

	rows, err := s.q.ListTaskDependenciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("taskdependency: list: %w", err)
	}
	out := make([]TaskDependency, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// Delete removes the dependency, scoped through its predecessor task's
// project owner. It returns ErrNotFound both when the dependency does not
// exist and when it belongs to another user.
func (s *Service) Delete(ctx context.Context, ownerID, dependencyID uuid.UUID) error {
	affected, err := s.q.DeleteTaskDependencyForOwner(ctx, db.DeleteTaskDependencyForOwnerParams{
		ID:          dependencyID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return fmt.Errorf("taskdependency: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ProjectID returns the project dependencyID belongs to (resolved through
// its predecessor task), with no owner check — only
// requireTokenResourceProject (internal/http, issue #66) uses this, to
// compare against a bearer token's own project. Every other method on this
// Service already scopes by owner; this one exists because a token has no
// owner to join against in the first place.
func (s *Service) ProjectID(ctx context.Context, dependencyID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetTaskDependencyProjectID(ctx, dependencyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("taskdependency: project id: %w", err)
	}
	return projectID, nil
}
