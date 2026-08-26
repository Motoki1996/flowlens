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
	"time"

	"github.com/flowlens/api/internal/assignee"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/fieldnorm"
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
	ErrInvalidName       = errors.New("backlog: name must be 1-100 characters")
	ErrInvalidSchedule   = errors.New("backlog: start date must not be after due date")
	ErrInvalidPriority   = errors.New("backlog: priority must be one of low, medium, high, urgent")
	ErrInvalidProgress   = errors.New("backlog: progress must be one of not_started, in_progress, on_hold, done")
	ErrInvalidBaseBranch = errors.New("backlog: baseBranch must be a valid git branch name, at most 255 characters")
	ErrInvalidScope      = errors.New("backlog: allowedScope/forbiddenScope must be at most 20000 characters")
	ErrLinkNotInProject  = errors.New("backlog: defaultLinkedGitlabProjectId must be a GitLab project linked to this project")
	ErrNotFound          = errors.New("backlog: not found")
	ErrForbidden         = errors.New("backlog: forbidden")
)

// Priority values, app-only and never synced to GitLab, mirroring
// internal/task's (a backlog's priority is independent of its tasks', per
// the 000010 migration). SortPriority is the ListFilter.Sort value that
// switches List's ORDER BY from creation order to priority rank.
const (
	PriorityLow    = fieldnorm.PriorityLow
	PriorityMedium = fieldnorm.PriorityMedium
	PriorityHigh   = fieldnorm.PriorityHigh
	PriorityUrgent = fieldnorm.PriorityUrgent

	SortPriority = fieldnorm.SortPriority
)

// Progress values, the backlog's own four-stage work state. App-only and
// never synced to GitLab, mirroring internal/task's — and, like a task's,
// independent of anything GitLab reports (see the 000011 migration). A
// backlog's progress is its own, not derived from its tasks'. SortProgress is
// the ListFilter.Sort value that switches List's ORDER BY from creation order
// to progress rank, running not_started first through done.
const (
	ProgressNotStarted = fieldnorm.ProgressNotStarted
	ProgressInProgress = fieldnorm.ProgressInProgress
	ProgressOnHold     = fieldnorm.ProgressOnHold
	ProgressDone       = fieldnorm.ProgressDone

	SortProgress = fieldnorm.SortProgress
)

// Status values, the backlog's own open/closed state (000036). Shaped as a
// task's status because it is the same concept one rung up — a backlog that
// has shipped, or been abandoned, is closed and leaves the collection view —
// but not the same *kind* of field: tasks.status mirrors the GitLab issue
// state and syncs both ways, while a backlog has no GitLab counterpart at all,
// so this is app-only end to end like Priority and Progress.
//
// Closing is deliberately not a statement about the backlog's tasks: it never
// cascades (see the 000036 migration). It is also not Progress — ProgressDone
// says the work finished, which an abandoned backlog never does.
//
// StatusAll is not a stored value: it is the ListFilter.Status escape hatch
// that turns the collection's open-only default off.
const (
	StatusOpen   = "open"
	StatusClosed = "closed"
	StatusAll    = "all"
)

// Actor kind values Update accepts to attribute a backlog_progress_events
// row (issue #173) to whoever changed progress — a bearer-token (agent)
// caller or a session (user) caller — mirroring internal/task's own
// ActorKindUser/ActorKindAgent. Like a task's, a backlog's progress is
// app-only and never moves via the GitLab sync path.
const (
	ActorKindUser  = "user"
	ActorKindAgent = "agent"
)

// Backlog is the API-facing representation of a project backlog. StartDate and
// DueOn are the backlog's planned period, drawn as one bar on the Backlog
// timeline. Both are app-only and never synced to GitLab — a backlog is not a
// GitLab milestone (see the 000008 migration).
type Backlog struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"projectId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Status is StatusOpen or StatusClosed, and ClosedAt the moment it last
	// became the latter (nil while open). Neither is writable through Update:
	// they move only via Close/Reopen, exactly as a task's do.
	Status    string     `json:"status"`
	ClosedAt  *time.Time `json:"closedAt"`
	StartDate *time.Time `json:"startDate"`
	DueOn     *time.Time `json:"dueOn"`
	Priority  string     `json:"priority"`
	Progress  string     `json:"progress"`
	// DefaultLinkedGitlabProjectID is the GitLab project a task filed in this
	// backlog gets its issue created in, overriding the project's own default
	// link. nil — the value every backlog starts with — means "use the project
	// default" (see the 000021 migration and internal/task.Service.Create).
	// It is read only when a task is created: moving a task into or out of a
	// backlog afterwards never moves an issue that already exists.
	DefaultLinkedGitlabProjectID *uuid.UUID `json:"defaultLinkedGitlabProjectId"`
	// BaseBranch is the branch tasks in this backlog are meant to branch from
	// during development (e.g. "main", "release/2.4"). Optional, app-only,
	// and never synced to GitLab — unlike a merge request's own base branch,
	// which is a fact synced from GitLab about an actual merge request (see
	// the 000024 migration).
	BaseBranch string `json:"baseBranch"`
	// AllowedScope/ForbiddenScope are the paths tasks filed in this backlog
	// may/may not touch — moved here from task_ai_contexts (000029
	// migration) because they describe a sub-area of the codebase, not one
	// task, and were being copy-pasted onto every task in a backlog.
	// Optional, app-only, never synced to GitLab, resolved into
	// GET /tasks/{taskID}/context through the task's backlog the same way
	// BaseBranch is.
	AllowedScope   string `json:"allowedScope"`
	ForbiddenScope string `json:"forbiddenScope"`
	// AssigneeUserID is the project member who owns this backlog (000031),
	// the same field a task carries — except a backlog has no GitLab
	// counterpart to mirror onto, so it is app-only end to end, like
	// BaseBranch and AllowedScope above. AssigneeUsername/AssigneeDisplayName
	// are resolved from users on read rather than stored; both are "" when
	// unassigned.
	AssigneeUserID      *uuid.UUID `json:"assigneeUserId"`
	AssigneeUsername    string     `json:"assigneeUsername"`
	AssigneeDisplayName string     `json:"assigneeDisplayName"`
	// TaskCount and ClosedTaskCount are the backlog's total and closed task
	// counts, computed by ListBacklogsByProject's LEFT JOIN aggregate (issue
	// #144) so the Backlog collection screen doesn't need to fetch every task
	// in the project just to show a count and a completion ratio. Populated
	// only by List — zero on every
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
		ID:                           row.ID,
		ProjectID:                    row.ProjectID,
		Name:                         row.Name,
		Description:                  row.Description,
		Status:                       row.Status,
		ClosedAt:                     timePtr(row.ClosedAt),
		StartDate:                    datePtr(row.StartDate),
		DueOn:                        datePtr(row.DueOn),
		Priority:                     row.Priority,
		Progress:                     row.Progress,
		DefaultLinkedGitlabProjectID: uuidPtr(row.DefaultLinkedGitlabProjectID),
		BaseBranch:                   row.BaseBranch,
		AllowedScope:                 row.AllowedScope,
		ForbiddenScope:               row.ForbiddenScope,
		AssigneeUserID:               uuidPtr(row.AssigneeUserID),
		CreatedAt:                    row.CreatedAt.Time,
		UpdatedAt:                    row.UpdatedAt.Time,
	}
}

// fromListRow maps a ListBacklogsByProject row, which additionally carries
// the LEFT JOIN's task counts, to the domain model.
func fromListRow(row db.ListBacklogsByProjectRow) Backlog {
	return Backlog{
		ID:                           row.ID,
		ProjectID:                    row.ProjectID,
		Name:                         row.Name,
		Description:                  row.Description,
		Status:                       row.Status,
		ClosedAt:                     timePtr(row.ClosedAt),
		StartDate:                    datePtr(row.StartDate),
		DueOn:                        datePtr(row.DueOn),
		Priority:                     row.Priority,
		Progress:                     row.Progress,
		DefaultLinkedGitlabProjectID: uuidPtr(row.DefaultLinkedGitlabProjectID),
		BaseBranch:                   row.BaseBranch,
		AllowedScope:                 row.AllowedScope,
		ForbiddenScope:               row.ForbiddenScope,
		AssigneeUserID:               uuidPtr(row.AssigneeUserID),
		TaskCount:                    row.TaskCount,
		ClosedTaskCount:              row.ClosedTaskCount,
		CreatedAt:                    row.CreatedAt.Time,
		UpdatedAt:                    row.UpdatedAt.Time,
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

// timePtr converts a nullable timestamptz — only ClosedAt today — to a
// pointer, the way datePtr does for a nullable date.
func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func uuidPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
}

func toUUID(v *uuid.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *v, Valid: true}
}

// The field rules below are internal/fieldnorm's, wrapped only to attach this
// package's own sentinel errors. internal/epic wraps the same functions: a
// backlog and an epic share these fields by design (see the 000032
// migration), and a second copy of the rules could only drift.

func validateSchedule(startDate, dueOn *time.Time) error {
	if err := fieldnorm.Schedule(startDate, dueOn); err != nil {
		return ErrInvalidSchedule
	}
	return nil
}

func normalizePriority(raw string) (string, error) {
	v, err := fieldnorm.Priority(raw)
	if err != nil {
		return "", ErrInvalidPriority
	}
	return v, nil
}

func normalizeProgress(raw string) (string, error) {
	v, err := fieldnorm.Progress(raw)
	if err != nil {
		return "", ErrInvalidProgress
	}
	return v, nil
}

func normalizeBaseBranch(raw string) (string, error) {
	v, err := fieldnorm.BaseBranch(raw)
	if err != nil {
		return "", ErrInvalidBaseBranch
	}
	return v, nil
}

func normalizeScope(raw string) (string, error) {
	v, err := fieldnorm.Scope(raw)
	if err != nil {
		return "", ErrInvalidScope
	}
	return v, nil
}

// Service manages backlogs inside projects owned by a single user.
type Service struct {
	q        db.Querier
	txRunner database.TxRunner
	projects *project.Service
}

// NewService constructs a backlog Service. projects is used to verify
// project access before any project-scoped operation. txRunner is used only
// by Update, to write a backlog_progress_events row (issue #173) in the
// same transaction as a progress change.
func NewService(q db.Querier, txRunner database.TxRunner, projects *project.Service) *Service {
	return &Service{q: q, txRunner: txRunner, projects: projects}
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

// validateLink checks that linkID, if set, is a GitLab project linked to
// projectID's own GitLab connection. Nothing in the schema can enforce this —
// linked_gitlab_projects reaches its project only through gitlab_connections —
// so a backlog pointing at another project's link would otherwise silently
// push issues somewhere the project has no business writing to. A link the
// caller cannot see is rejected the same as one that does not exist.
func (s *Service) validateLink(ctx context.Context, ownerID, projectID uuid.UUID, linkID *uuid.UUID) error {
	if linkID == nil {
		return nil
	}
	_, err := s.q.GetLinkedGitlabProjectInProjectForOwner(ctx, db.GetLinkedGitlabProjectInProjectForOwnerParams{
		ID:          *linkID,
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotInProject
		}
		return fmt.Errorf("backlog: validate linked gitlab project: %w", err)
	}
	return nil
}

// normalizeName trims raw and enforces the 1-100 character rule.
func normalizeName(raw string) (string, error) {
	name, err := fieldnorm.Name(raw)
	if err != nil {
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
	// DefaultLinkedGitlabProjectID is optional: nil leaves the backlog on the
	// project's default link. A link outside this project is rejected with
	// ErrLinkNotInProject.
	DefaultLinkedGitlabProjectID *uuid.UUID
	// BaseBranch is optional; empty means "not set". Validated as a git
	// branch name when non-empty.
	BaseBranch string
	// AllowedScope/ForbiddenScope are optional; empty means "not set".
	// Capped at maxScopeFieldLength, otherwise unrestricted free text.
	AllowedScope   string
	ForbiddenScope string
	// AssigneeUserID is optional: nil leaves the backlog unassigned. A user
	// who is not a member of the project is rejected with
	// assignee.ErrNotMember.
	AssigneeUserID *uuid.UUID
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
	if err := s.validateLink(ctx, ownerID, projectID, p.DefaultLinkedGitlabProjectID); err != nil {
		return Backlog{}, err
	}
	baseBranch, err := normalizeBaseBranch(p.BaseBranch)
	if err != nil {
		return Backlog{}, err
	}
	allowedScope, err := normalizeScope(p.AllowedScope)
	if err != nil {
		return Backlog{}, err
	}
	forbiddenScope, err := normalizeScope(p.ForbiddenScope)
	if err != nil {
		return Backlog{}, err
	}
	if p.AssigneeUserID != nil {
		if err := assignee.ValidateMember(ctx, s.q, projectID, *p.AssigneeUserID); err != nil {
			return Backlog{}, err
		}
	}
	row, err := s.q.CreateBacklog(ctx, db.CreateBacklogParams{
		ProjectID:                    projectID,
		Name:                         normalized,
		Description:                  p.Description,
		StartDate:                    toDate(p.StartDate),
		DueOn:                        toDate(p.DueOn),
		Priority:                     priority,
		Progress:                     progress,
		DefaultLinkedGitlabProjectID: toUUID(p.DefaultLinkedGitlabProjectID),
		BaseBranch:                   baseBranch,
		AllowedScope:                 allowedScope,
		ForbiddenScope:               forbiddenScope,
		AssigneeUserID:               toUUID(p.AssigneeUserID),
	})
	if err != nil {
		return Backlog{}, fmt.Errorf("backlog: create: %w", err)
	}
	b := fromRow(row)
	if err := s.attachAssigneeName(ctx, &b); err != nil {
		return Backlog{}, err
	}
	return b, nil
}

// ListFilter narrows List to a subset of a project's backlogs. The zero value
// means "open backlogs only, creation order" — note that Status is the one
// field whose zero value is a filter rather than the absence of one.
type ListFilter struct {
	// Status is StatusOpen, StatusClosed, StatusAll, or "" — and "" is the
	// one filter field whose zero value is not "no filter": it resolves to
	// StatusOpen, so a closed backlog drops out of the collection without the
	// caller asking. StatusAll is how a caller asks for both, and is what the
	// web app's Status filter sends when it is set to "All statuses". Getting
	// a closed backlog by ID always works; only the list hides it.
	Status   string
	Priority string // one of the Priority* constants, or "" (no filter)
	Progress string // one of the Progress* constants, or "" (no filter)
	// AssigneeUserID, when non-nil, only returns backlogs assigned to that
	// user; AssigneeUnassigned only those assigned to nobody. Unlike a task's
	// (internal/task.ListFilter), there is no GitLab axis to OR against — a
	// backlog has no GitLab counterpart. Mutually exclusive.
	AssigneeUserID     *uuid.UUID
	AssigneeUnassigned bool
	// Sort is "" (created_at ASC, the backlogs' creation order),
	// SortPriority (priority rank DESC, then the same tiebreak) or
	// SortProgress (progress rank ASC, then the same tiebreak).
	Sort string
}

// resolveStatusFilter turns a ListFilter.Status into the value the query
// wants, where "" means "no status filter". The two are not the same
// vocabulary: an absent filter means open-only here, and only StatusAll
// disables the filter entirely.
func resolveStatusFilter(v string) string {
	switch v {
	case "":
		return StatusOpen
	case StatusAll:
		return ""
	default:
		return v
	}
}

// List returns projectID's backlogs matching filter, in creation order (or
// by priority when filter.Sort is SortPriority). Closed backlogs are omitted
// unless filter.Status asks for them. It returns ErrNotFound if projectID
// does not exist or belongs to another user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID, filter ListFilter) ([]Backlog, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.q.ListBacklogsByProject(ctx, db.ListBacklogsByProjectParams{
		ProjectID:          projectID,
		Status:             resolveStatusFilter(filter.Status),
		Priority:           filter.Priority,
		Progress:           filter.Progress,
		AssigneeUserID:     toUUID(filter.AssigneeUserID),
		AssigneeUnassigned: filter.AssigneeUnassigned,
		SortByPriority:     filter.Sort == SortPriority,
		SortByProgress:     filter.Sort == SortProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("backlog: list: %w", err)
	}
	out := make([]Backlog, len(rows))
	for i, row := range rows {
		out[i] = fromListRow(row)
	}
	if err := s.attachAssigneeNamesToList(ctx, out); err != nil {
		return nil, err
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
	b := fromRow(row)
	if err := s.attachAssigneeName(ctx, &b); err != nil {
		return Backlog{}, err
	}
	return b, nil
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

// UpdateParams are the attributes Update writes. Name and Description are
// always overwritten; the two dates are Optional so a caller that
// only renames a backlog — the rename form in the web UI does exactly that —
// leaves its planned period untouched rather than clearing it. An explicit
// null clears the date.
type UpdateParams struct {
	// Name and Description follow the same absent-keeps-the-current-value
	// rule as every field below, which is what makes this a real partial
	// update: a caller changing only a backlog's priority sends only that,
	// and must not have the backlog renamed to "" behind it. An explicit
	// empty Name is still rejected — a backlog has to be called something.
	Name        optional.Optional[string]
	Description optional.Optional[string]
	StartDate   optional.Optional[*time.Time]
	DueOn       optional.Optional[*time.Time]
	// Priority left absent keeps the backlog's current priority; an
	// explicit empty string resets it to PriorityMedium, the same as an
	// absent Priority on CreateParams.
	Priority optional.Optional[string]
	// Progress follows the same absent/explicit-empty rule as Priority,
	// resetting to ProgressNotStarted when explicitly empty.
	Progress optional.Optional[string]
	// DefaultLinkedGitlabProjectID is Optional for the same reason the dates
	// are: a caller that only renames a backlog must not silently reset where
	// its tasks' issues go. An explicit null falls the backlog back to the
	// project's default link.
	DefaultLinkedGitlabProjectID optional.Optional[*uuid.UUID]
	// BaseBranch follows the same absent/explicit-empty rule as Priority and
	// Progress: absent keeps the current value, an explicit empty string
	// clears it back to "not set".
	BaseBranch optional.Optional[string]
	// AllowedScope/ForbiddenScope follow the same absent/explicit-empty rule
	// as BaseBranch.
	AllowedScope   optional.Optional[string]
	ForbiddenScope optional.Optional[string]
	// AssigneeUserID is Optional for the same reason the dates are: renaming
	// a backlog must not silently unassign it. An explicit null unassigns.
	AssigneeUserID optional.Optional[*uuid.UUID]
}

// Update overwrites name and description, and applies whichever of
// the dates the caller set. Ownership is enforced by the query, so a non-owner
// gets ErrNotFound and nothing is written.
//
// actorKind (ActorKindUser for a session caller, ActorKindAgent for a
// bearer-token caller) attributes a backlog_progress_events row (issue
// #173), written in the same transaction only when progress actually
// changes — this is the sole place that table is ever written, mirroring
// internal/task.Service.Update's own progress-event insertion point.
func (s *Service) Update(ctx context.Context, ownerID, backlogID uuid.UUID, p UpdateParams, actorKind string) (Backlog, error) {
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
	normalized, err := normalizeName(p.Name.Or(current.Name))
	if err != nil {
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
	// Only a link the caller actually sent is re-validated: the stored one
	// was checked when it was written, and a link that has since been
	// unlinked is already NULL here (ON DELETE SET NULL).
	link := p.DefaultLinkedGitlabProjectID.Or(current.DefaultLinkedGitlabProjectID)
	if _, changed := p.DefaultLinkedGitlabProjectID.Get(); changed {
		if err := s.validateLink(ctx, ownerID, current.ProjectID, link); err != nil {
			return Backlog{}, err
		}
	}

	baseBranch, err := normalizeBaseBranch(p.BaseBranch.Or(current.BaseBranch))
	if err != nil {
		return Backlog{}, err
	}
	allowedScope, err := normalizeScope(p.AllowedScope.Or(current.AllowedScope))
	if err != nil {
		return Backlog{}, err
	}
	forbiddenScope, err := normalizeScope(p.ForbiddenScope.Or(current.ForbiddenScope))
	if err != nil {
		return Backlog{}, err
	}

	// Only an assignee the caller actually sent is re-validated, the same rule
	// the link above follows: the stored one was checked when it was written,
	// and a member who has since left the project is already NULL here (ON
	// DELETE SET NULL only covers deletion, so a removed member's assignment
	// does survive — deliberately, since removing someone from a project
	// should not silently rewrite what they were working on).
	newAssignee := p.AssigneeUserID.Or(current.AssigneeUserID)
	if _, changed := p.AssigneeUserID.Get(); changed && newAssignee != nil {
		if err := assignee.ValidateMember(ctx, s.q, current.ProjectID, *newAssignee); err != nil {
			return Backlog{}, err
		}
	}

	progressChanged := current.Progress != progress

	var result Backlog
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := q.UpdateBacklogForOwner(ctx, db.UpdateBacklogForOwnerParams{
			ID:                           backlogID,
			OwnerUserID:                  ownerID,
			Name:                         normalized,
			Description:                  p.Description.Or(current.Description),
			StartDate:                    toDate(startDate),
			DueOn:                        toDate(dueOn),
			Priority:                     priority,
			Progress:                     progress,
			DefaultLinkedGitlabProjectID: toUUID(link),
			BaseBranch:                   baseBranch,
			AllowedScope:                 allowedScope,
			ForbiddenScope:               forbiddenScope,
			AssigneeUserID:               toUUID(newAssignee),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("backlog: update: %w", err)
		}
		result = fromRow(row)

		if progressChanged {
			actorUserID := &ownerID
			if actorKind == ActorKindAgent {
				actorUserID = nil
			}
			if _, err := q.CreateBacklogProgressEvent(ctx, db.CreateBacklogProgressEventParams{
				BacklogID:    backlogID,
				FromProgress: current.Progress,
				ToProgress:   progress,
				ActorKind:    actorKind,
				ActorUserID:  toUUID(actorUserID),
			}); err != nil {
				return fmt.Errorf("backlog: update: record progress event: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Backlog{}, err
	}
	if err := s.attachAssigneeName(ctx, &result); err != nil {
		return Backlog{}, err
	}
	return result, nil
}

// Close marks the backlog closed and stamps closed_at: it has shipped, or
// been abandoned, and should leave the collection view. Closing an
// already-closed backlog is a no-op, so closed_at never moves on a re-close —
// the same rule internal/task.Service.Close follows.
//
// It does not cascade. The backlog's epics and tasks are left exactly as they
// were, deliberately: see the 000036 migration for why closing them by proxy
// would either invent completions in internal/velocity or close GitLab issues
// nobody asked to close. Leftover work is moved to another backlog instead.
//
// Nothing is enqueued for GitLab either — a backlog has no issue behind it.
func (s *Service) Close(ctx context.Context, ownerID, backlogID uuid.UUID) (Backlog, error) {
	return s.setStatus(ctx, ownerID, backlogID, StatusClosed)
}

// Reopen marks the backlog open again and clears closed_at, bringing it back
// into the collection view. Reopening an already-open backlog is a no-op, and
// it no more cascades than Close does: a task closed while its backlog was
// closed stays closed.
func (s *Service) Reopen(ctx context.Context, ownerID, backlogID uuid.UUID) (Backlog, error) {
	return s.setStatus(ctx, ownerID, backlogID, StatusOpen)
}

// setStatus is the half Close and Reopen share: read the current row (which
// is also what enforces visibility), return it untouched when the status is
// already the requested one, and otherwise run the matching statement.
func (s *Service) setStatus(ctx context.Context, ownerID, backlogID uuid.UUID, status string) (Backlog, error) {
	current, err := s.Get(ctx, ownerID, backlogID)
	if err != nil {
		return Backlog{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return Backlog{}, err
	}
	if current.Status == status {
		return current, nil
	}

	var row db.Backlog
	if status == StatusClosed {
		row, err = s.q.CloseBacklogForOwner(ctx, db.CloseBacklogForOwnerParams{ID: backlogID, OwnerUserID: ownerID})
	} else {
		row, err = s.q.ReopenBacklogForOwner(ctx, db.ReopenBacklogForOwnerParams{ID: backlogID, OwnerUserID: ownerID})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backlog{}, ErrNotFound
		}
		return Backlog{}, fmt.Errorf("backlog: set status %s: %w", status, err)
	}

	result := fromRow(row)
	if err := s.attachAssigneeName(ctx, &result); err != nil {
		return Backlog{}, err
	}
	return result, nil
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
