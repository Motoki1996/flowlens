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
	"github.com/flowlens/api/internal/optional"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is returned both when a backlog/project does not exist
// and when it belongs to another user.
var (
	ErrInvalidName        = errors.New("backlog: name must be 1-100 characters")
	ErrInvalidSchedule    = errors.New("backlog: start date must not be after due date")
	ErrInvalidPriority    = errors.New("backlog: priority must be one of low, medium, high, urgent")
	ErrInvalidProgress    = errors.New("backlog: progress must be one of not_started, in_progress, on_hold, done")
	ErrNotFound           = errors.New("backlog: not found")
	ErrForbidden          = errors.New("backlog: forbidden")
	ErrBacklogIDsMismatch = errors.New("backlog: backlogIds must exactly match the project's current backlogs")
)

// Priority values, app-only and never synced to GitLab, mirroring
// internal/task's (a backlog's priority is independent of its tasks', per
// the 000010 migration). SortPriority is the ListFilter.Sort value that
// switches List's ORDER BY from position to priority rank.
const (
	PriorityLow    = "low"
	PriorityMedium = "medium"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"

	SortPriority = "priority"
)

// Progress values, the backlog's own four-stage work state. App-only and
// never synced to GitLab, mirroring internal/task's — and, like a task's,
// independent of anything GitLab reports (see the 000011 migration). A
// backlog's progress is its own, not derived from its tasks'. SortProgress is
// the ListFilter.Sort value that switches List's ORDER BY from position to
// progress rank, running not_started first through done.
const (
	ProgressNotStarted = "not_started"
	ProgressInProgress = "in_progress"
	ProgressOnHold     = "on_hold"
	ProgressDone       = "done"

	SortProgress = "progress"
)

// Backlog is the API-facing representation of a project backlog. StartDate and
// DueOn are the backlog's planned period, drawn as one bar on the Backlog
// timeline. Both are app-only and never synced to GitLab — a backlog is not a
// GitLab milestone (see the 000008 migration).
type Backlog struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"projectId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Position    int32      `json:"position"`
	StartDate   *time.Time `json:"startDate"`
	DueOn       *time.Time `json:"dueOn"`
	Priority    string     `json:"priority"`
	Progress    string     `json:"progress"`
	// TaskCount and ClosedTaskCount are the backlog's total and closed task
	// counts, computed by ListBacklogsByProject's LEFT JOIN aggregate (issue
	// #144) so the Backlog collection screen doesn't need to fetch every task
	// in the project just to show a count and a completion ratio. Populated
	// only by List (and Reorder, which returns List's result) — zero on every
	// other Backlog response (Create/Get/Update), which don't compute the
	// join, mirroring internal/project's FailedSyncTaskCount.
	TaskCount       int64     `json:"taskCount"`
	ClosedTaskCount int64     `json:"closedTaskCount"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// fromRow maps a database row to the domain model. TaskCount and
// ClosedTaskCount are left zero: db.Backlog carries no join, unlike
// db.ListBacklogsByProjectRow (see fromListRow).
func fromRow(row db.Backlog) Backlog {
	return Backlog{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		Name:        row.Name,
		Description: row.Description,
		Position:    row.Position,
		StartDate:   datePtr(row.StartDate),
		DueOn:       datePtr(row.DueOn),
		Priority:    row.Priority,
		Progress:    row.Progress,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}

// fromListRow maps a ListBacklogsByProject row, which additionally carries
// the LEFT JOIN's task counts, to the domain model.
func fromListRow(row db.ListBacklogsByProjectRow) Backlog {
	return Backlog{
		ID:              row.ID,
		ProjectID:       row.ProjectID,
		Name:            row.Name,
		Description:     row.Description,
		Position:        row.Position,
		StartDate:       datePtr(row.StartDate),
		DueOn:           datePtr(row.DueOn),
		Priority:        row.Priority,
		Progress:        row.Progress,
		TaskCount:       row.TaskCount,
		ClosedTaskCount: row.ClosedTaskCount,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}
}

func datePtr(v pgtype.Date) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func toDate(v *time.Time) pgtype.Date {
	if v == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *v, Valid: true}
}

// validateSchedule rejects a period that ends before it starts. Either date
// alone is fine: a backlog with only a due date is a deadline without a
// committed start, and the timeline draws it as a single day.
func validateSchedule(startDate, dueOn *time.Time) error {
	if startDate != nil && dueOn != nil && startDate.After(*dueOn) {
		return ErrInvalidSchedule
	}
	return nil
}

// normalizePriority defaults an empty raw to PriorityMedium — Create leaves
// it unset when the caller doesn't specify one, and Update's Optional
// resolves an explicit empty string the same way — and otherwise rejects
// anything outside the fixed set.
func normalizePriority(raw string) (string, error) {
	switch raw {
	case "":
		return PriorityMedium, nil
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return raw, nil
	default:
		return "", ErrInvalidPriority
	}
}

// normalizeProgress defaults an empty raw to ProgressNotStarted, the same
// absent/explicit-empty rule normalizePriority follows, and otherwise rejects
// anything outside the fixed set.
func normalizeProgress(raw string) (string, error) {
	switch raw {
	case "":
		return ProgressNotStarted, nil
	case ProgressNotStarted, ProgressInProgress, ProgressOnHold, ProgressDone:
		return raw, nil
	default:
		return "", ErrInvalidProgress
	}
}

// Service manages backlogs inside projects owned by a single user.
type Service struct {
	q        db.Querier
	projects *project.Service
}

// NewService constructs a backlog Service. projects is used to verify
// project access before any project-scoped operation.
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
		return fmt.Errorf("backlog: authorize: %w", err)
	}
}

// normalizeName trims raw and enforces the 1-100 character rule.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return "", ErrInvalidName
	}
	return name, nil
}

// CreateParams are the attributes of a new backlog. StartDate and DueOn are
// both optional — a backlog with no planned period simply doesn't appear on
// the timeline until one is set.
type CreateParams struct {
	Name        string
	Description string
	StartDate   *time.Time
	DueOn       *time.Time
	// Priority defaults to PriorityMedium when empty.
	Priority string
	// Progress defaults to ProgressNotStarted when empty.
	Progress string
}

// Create validates name and creates a backlog at the end of projectID's
// backlog order. It returns ErrNotFound if projectID does not exist or
// belongs to another user.
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, p CreateParams) (Backlog, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return Backlog{}, err
	}

	normalized, err := normalizeName(p.Name)
	if err != nil {
		return Backlog{}, err
	}
	if err := validateSchedule(p.StartDate, p.DueOn); err != nil {
		return Backlog{}, err
	}
	priority, err := normalizePriority(p.Priority)
	if err != nil {
		return Backlog{}, err
	}
	progress, err := normalizeProgress(p.Progress)
	if err != nil {
		return Backlog{}, err
	}
	row, err := s.q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID:   projectID,
		Name:        normalized,
		Description: p.Description,
		StartDate:   toDate(p.StartDate),
		DueOn:       toDate(p.DueOn),
		Priority:    priority,
		Progress:    progress,
	})
	if err != nil {
		return Backlog{}, fmt.Errorf("backlog: create: %w", err)
	}
	return fromRow(row), nil
}

// ListFilter narrows List to a subset of a project's backlogs. The zero
// value means "no filter, default (position) order".
type ListFilter struct {
	Priority string // one of the Priority* constants, or "" (no filter)
	Progress string // one of the Progress* constants, or "" (no filter)
	// Sort is "" (position ASC, created_at ASC, the manual order),
	// SortPriority (priority rank DESC, then the same tiebreak) or
	// SortProgress (progress rank ASC, then the same tiebreak).
	Sort string
}

// List returns projectID's backlogs matching filter, ordered by position (or
// by priority when filter.Sort is SortPriority). It returns ErrNotFound if
// projectID does not exist or belongs to another user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID, filter ListFilter) ([]Backlog, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.q.ListBacklogsByProject(ctx, db.ListBacklogsByProjectParams{
		ProjectID:      projectID,
		Priority:       filter.Priority,
		Progress:       filter.Progress,
		SortByPriority: filter.Sort == SortPriority,
		SortByProgress: filter.Sort == SortProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("backlog: list: %w", err)
	}
	out := make([]Backlog, len(rows))
	for i, row := range rows {
		out[i] = fromListRow(row)
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

// ProjectID returns the project backlogID belongs to, with no owner check —
// only requireTokenResourceProject (internal/http, issue #66) uses this, to
// compare against a bearer token's own project. Every other method on this
// Service already scopes by owner; this one exists because a token has no
// owner to join against in the first place.
func (s *Service) ProjectID(ctx context.Context, backlogID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetBacklogProjectID(ctx, backlogID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("backlog: project id: %w", err)
	}
	return projectID, nil
}

// UpdateParams are the attributes Update writes. Name, Description and
// Position are always overwritten; the two dates are Optional so a caller that
// only renames a backlog — the rename form in the web UI does exactly that —
// leaves its planned period untouched rather than clearing it. An explicit
// null clears the date.
type UpdateParams struct {
	Name        string
	Description string
	Position    int32
	StartDate   optional.Optional[*time.Time]
	DueOn       optional.Optional[*time.Time]
	// Priority left absent keeps the backlog's current priority; an
	// explicit empty string resets it to PriorityMedium, the same as an
	// absent Priority on CreateParams.
	Priority optional.Optional[string]
	// Progress follows the same absent/explicit-empty rule as Priority,
	// resetting to ProgressNotStarted when explicitly empty.
	Progress optional.Optional[string]
}

// Update overwrites name, description and position, and applies whichever of
// the dates the caller set. Ownership is enforced by the query, so a non-owner
// gets ErrNotFound and nothing is written.
func (s *Service) Update(ctx context.Context, ownerID, backlogID uuid.UUID, p UpdateParams) (Backlog, error) {
	normalized, err := normalizeName(p.Name)
	if err != nil {
		return Backlog{}, err
	}

	// The UPDATE writes every column, so absent dates have to be resolved
	// against the stored row first. Get is viewer-scoped, so a foreign
	// backlog stops here with ErrNotFound before anything is written; the
	// member-minimum write check itself happens right after, once
	// current.ProjectID is known.
	current, err := s.Get(ctx, ownerID, backlogID)
	if err != nil {
		return Backlog{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return Backlog{}, err
	}
	startDate := p.StartDate.Or(current.StartDate)
	dueOn := p.DueOn.Or(current.DueOn)
	if err := validateSchedule(startDate, dueOn); err != nil {
		return Backlog{}, err
	}
	priority, err := normalizePriority(p.Priority.Or(current.Priority))
	if err != nil {
		return Backlog{}, err
	}
	progress, err := normalizeProgress(p.Progress.Or(current.Progress))
	if err != nil {
		return Backlog{}, err
	}

	row, err := s.q.UpdateBacklogForOwner(ctx, db.UpdateBacklogForOwnerParams{
		ID:          backlogID,
		OwnerUserID: ownerID,
		Name:        normalized,
		Description: p.Description,
		Position:    p.Position,
		StartDate:   toDate(startDate),
		DueOn:       toDate(dueOn),
		Priority:    priority,
		Progress:    progress,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backlog{}, ErrNotFound
		}
		return Backlog{}, fmt.Errorf("backlog: update: %w", err)
	}
	return fromRow(row), nil
}

// Reorder resequences the position of every backlog in projectID to match
// backlogIDs' order — position 0 for the first ID, 1 for the second, and so
// on. backlogIDs must be exactly the project's current backlog set (same
// length, no duplicates, nothing missing or foreign), or it returns
// ErrBacklogIDsMismatch without writing anything, the same all-or-nothing
// guard internal/task.Service.Reorder applies to a backlog's tasks (issue
// #79).
func (s *Service) Reorder(ctx context.Context, ownerID, projectID uuid.UUID, backlogIDs []uuid.UUID) ([]Backlog, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return nil, err
	}

	current, err := s.q.ListBacklogsByProject(ctx, db.ListBacklogsByProjectParams{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("backlog: reorder: %w", err)
	}
	if !sameBacklogIDSet(current, backlogIDs) {
		return nil, ErrBacklogIDsMismatch
	}

	if err := s.q.ReorderBacklogs(ctx, db.ReorderBacklogsParams{
		BacklogIds: backlogIDs,
		ProjectID:  projectID,
	}); err != nil {
		return nil, fmt.Errorf("backlog: reorder: %w", err)
	}

	return s.List(ctx, ownerID, projectID, ListFilter{})
}

// sameBacklogIDSet reports whether backlogIDs is exactly current's IDs, in
// any order: same length, no duplicates, nothing missing or foreign.
func sameBacklogIDSet(current []db.ListBacklogsByProjectRow, backlogIDs []uuid.UUID) bool {
	if len(current) != len(backlogIDs) {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(backlogIDs))
	for _, id := range backlogIDs {
		if _, dup := seen[id]; dup {
			return false
		}
		seen[id] = struct{}{}
	}
	for _, b := range current {
		if _, ok := seen[b.ID]; !ok {
			return false
		}
	}
	return true
}

// Delete removes the backlog. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted. Tasks in the backlog
// are not deleted: the schema's ON DELETE SET NULL drops them to
// unfiled (backlog_id = NULL).
func (s *Service) Delete(ctx context.Context, ownerID, backlogID uuid.UUID) error {
	current, err := s.Get(ctx, ownerID, backlogID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return err
	}
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
