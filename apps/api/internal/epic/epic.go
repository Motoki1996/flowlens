// Package epic contains the epic domain model and the service that manages
// the epics inside a project.
//
// An epic is the optional rung between a backlog and its tasks (000032
// migration): the coarse unit — one screen, one endpoint group — a refined
// backlog is first cut into, before each of those is broken down into tasks
// someone actually works. It is deliberately shaped as "a backlog that lives
// inside a backlog": every field here exists on internal/backlog.Backlog with
// the same meaning, minus size (an epic's size is the sum of its tasks'), plus
// BacklogID.
//
// Epics carry no owner column of their own: every method takes the acting
// user's ID and ownership is always checked through the parent project, the
// same posture as internal/backlog. An epic belonging to another user's
// project is indistinguishable from a missing one (ErrNotFound).
package epic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/assignee"
	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/optional"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is returned both when an epic/project does not exist and
// when it belongs to another user.
var (
	ErrInvalidName         = errors.New("epic: name must be 1-100 characters")
	ErrInvalidSchedule     = errors.New("epic: start date must not be after due date")
	ErrInvalidPriority     = errors.New("epic: priority must be one of low, medium, high, urgent")
	ErrInvalidProgress     = errors.New("epic: progress must be one of not_started, in_progress, on_hold, done")
	ErrInvalidBaseBranch   = errors.New("epic: baseBranch must be a valid git branch name, at most 255 characters")
	ErrInvalidScope        = errors.New("epic: allowedScope/forbiddenScope must be at most 20000 characters")
	ErrInvalidEstimate     = errors.New("epic: estimatedPoints must be a positive integer")
	ErrLinkNotInProject    = errors.New("epic: defaultLinkedGitlabProjectId must be a GitLab project linked to this project")
	ErrBacklogNotInProject = errors.New("epic: backlogId must be a backlog in this project")
	ErrTaskNotInProject    = errors.New("epic: taskIds must all be tasks in this project")
	ErrNotFound            = errors.New("epic: not found")
	ErrForbidden           = errors.New("epic: forbidden")
)

// ErrAssigneeNotMember re-exports internal/assignee's sentinel so handlers can
// map it without importing that package directly.
var ErrAssigneeNotMember = assignee.ErrNotMember

// Priority and Progress values, and the ListFilter.Sort values that switch
// List's ORDER BY. These are internal/backlog's own constants rather than
// copies: an epic's priority and progress mean exactly what a backlog's do,
// and a second set of string literals could only drift from the CHECK
// constraint they share.
const (
	PriorityLow    = backlog.PriorityLow
	PriorityMedium = backlog.PriorityMedium
	PriorityHigh   = backlog.PriorityHigh
	PriorityUrgent = backlog.PriorityUrgent

	ProgressNotStarted = backlog.ProgressNotStarted
	ProgressInProgress = backlog.ProgressInProgress
	ProgressOnHold     = backlog.ProgressOnHold
	ProgressDone       = backlog.ProgressDone

	SortPriority = backlog.SortPriority
	SortProgress = backlog.SortProgress
)

// Epic is the API-facing representation of an epic.
type Epic struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"projectId"`
	// BacklogID is the backlog this epic belongs to. nil means unfiled — the
	// Unclassified group, exactly as a task with no backlog is. An epic
	// cannot be filed in another project's backlog (ErrBacklogNotInProject).
	BacklogID   *uuid.UUID `json:"backlogId"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	StartDate   *time.Time `json:"startDate"`
	DueOn       *time.Time `json:"dueOn"`
	Priority    string     `json:"priority"`
	Progress    string     `json:"progress"`
	// DefaultLinkedGitlabProjectID is the GitLab project a task filed in this
	// epic gets its issue created in, overriding its backlog's own override of
	// the project default. nil means "fall through to the backlog". Read only
	// when a task is created: moving a task between epics afterwards never
	// moves an issue that already exists.
	DefaultLinkedGitlabProjectID *uuid.UUID `json:"defaultLinkedGitlabProjectId"`
	// BaseBranch is the branch tasks in this epic are meant to branch from,
	// and the field this whole rung exists for. Optional, app-only, never
	// synced to GitLab. Resolved epic-first, backlog-second into
	// GET /api/v1/tasks/{taskID}/context.
	BaseBranch string `json:"baseBranch"`
	// AllowedScope/ForbiddenScope are the paths tasks in this epic may/may not
	// touch, resolved the same way BaseBranch is.
	AllowedScope   string `json:"allowedScope"`
	ForbiddenScope string `json:"forbiddenScope"`
	// EstimatedPoints is the epic's *pre-breakdown* estimate: how much work it
	// is expected to be, in the same raw points internal/velocity weights a
	// task's size onto, guessed at the moment the epic is cut out of its
	// backlog and before any task exists. nil means nobody has estimated it,
	// which is deliberately distinct from any number including zero — hence
	// the > 0 CHECK on the column (000033).
	//
	// It is *not* a size. An epic has no size field on purpose: an epic's size
	// is the sum of its tasks'. This is the value that stands in until those
	// tasks exist and loses authority the moment they do — see EffectivePoints
	// — and it is never cleared or overwritten when they appear, since the
	// estimate beside the eventual real breakdown is the only thing an
	// estimate-vs-actual calibration could ever be built from.
	EstimatedPoints *int `json:"estimatedPoints"`
	// AssigneeUserID is the project member who owns this epic. App-only end to
	// end, with no GitLab bridge — a backlog's rule, not a task's, since an
	// epic has no GitLab counterpart to mirror onto. AssigneeUsername/
	// AssigneeDisplayName are resolved from users on read rather than stored.
	AssigneeUserID      *uuid.UUID `json:"assigneeUserId"`
	AssigneeUsername    string     `json:"assigneeUsername"`
	AssigneeDisplayName string     `json:"assigneeDisplayName"`
	// TaskCount and ClosedTaskCount come from ListEpicsByProject's LEFT JOIN
	// aggregate, so the Epic collection screen doesn't fetch every task just
	// to show a count and a completion ratio. Populated only by List — zero
	// on every other response, mirroring internal/backlog.
	TaskCount       int64     `json:"taskCount"`
	ClosedTaskCount int64     `json:"closedTaskCount"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func fromRow(row db.Epic) Epic {
	return Epic{
		ID:                           row.ID,
		ProjectID:                    row.ProjectID,
		BacklogID:                    uuidPtr(row.BacklogID),
		Name:                         row.Name,
		Description:                  row.Description,
		StartDate:                    datePtr(row.StartDate),
		DueOn:                        datePtr(row.DueOn),
		Priority:                     row.Priority,
		Progress:                     row.Progress,
		DefaultLinkedGitlabProjectID: uuidPtr(row.DefaultLinkedGitlabProjectID),
		BaseBranch:                   row.BaseBranch,
		AllowedScope:                 row.AllowedScope,
		ForbiddenScope:               row.ForbiddenScope,
		EstimatedPoints:              intPtr(row.EstimatedPoints),
		AssigneeUserID:               uuidPtr(row.AssigneeUserID),
		CreatedAt:                    row.CreatedAt.Time,
		UpdatedAt:                    row.UpdatedAt.Time,
	}
}

// fromListRow maps a ListEpicsByProject row, which additionally carries the
// LEFT JOIN's task counts, to the domain model.
func fromListRow(row db.ListEpicsByProjectRow) Epic {
	return Epic{
		ID:                           row.ID,
		ProjectID:                    row.ProjectID,
		BacklogID:                    uuidPtr(row.BacklogID),
		Name:                         row.Name,
		Description:                  row.Description,
		StartDate:                    datePtr(row.StartDate),
		DueOn:                        datePtr(row.DueOn),
		Priority:                     row.Priority,
		Progress:                     row.Progress,
		DefaultLinkedGitlabProjectID: uuidPtr(row.DefaultLinkedGitlabProjectID),
		BaseBranch:                   row.BaseBranch,
		AllowedScope:                 row.AllowedScope,
		ForbiddenScope:               row.ForbiddenScope,
		EstimatedPoints:              intPtr(row.EstimatedPoints),
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

func intPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

func toInt4(v *int) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true}
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

// Service manages epics inside a project.
type Service struct {
	q        db.Querier
	txRunner database.TxRunner
	projects *project.Service
}

// NewService constructs an epic Service. projects verifies project access
// before any project-scoped operation. txRunner is used only by Update, to
// move the epic's tasks to a new backlog in the same transaction as the epic
// itself (see MoveEpicTasksToBacklog).
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
		return fmt.Errorf("epic: authorize: %w", err)
	}
}

// validateBacklog checks that backlogID, if set, is a backlog in projectID.
// The FK only guarantees the backlog exists, not that it belongs to this
// project, and an epic filed in another project's backlog would appear in
// that project's screens while carrying this project's tasks.
func (s *Service) validateBacklog(ctx context.Context, ownerID, projectID uuid.UUID, backlogID *uuid.UUID) error {
	if backlogID == nil {
		return nil
	}
	row, err := s.q.GetBacklogForOwner(ctx, db.GetBacklogForOwnerParams{
		ID:          *backlogID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBacklogNotInProject
		}
		return fmt.Errorf("epic: validate backlog: %w", err)
	}
	if row.ProjectID != projectID {
		return ErrBacklogNotInProject
	}
	return nil
}

// validateLink checks that linkID, if set, is a GitLab project linked to
// projectID's own GitLab connection — the same rule, and the same reason, as
// internal/backlog.Service.validateLink.
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
		return fmt.Errorf("epic: validate linked gitlab project: %w", err)
	}
	return nil
}

// CreateParams are the attributes of a new epic. Every optional field means
// the same thing it does on internal/backlog.CreateParams.
type CreateParams struct {
	// BacklogID is optional: nil leaves the epic unfiled. A backlog outside
	// this project is rejected with ErrBacklogNotInProject.
	BacklogID   *uuid.UUID
	Name        string
	Description string
	StartDate   *time.Time
	DueOn       *time.Time
	// Priority defaults to PriorityMedium when empty.
	Priority string
	// Progress defaults to ProgressNotStarted when empty.
	Progress string
	// DefaultLinkedGitlabProjectID is optional: nil falls the epic through to
	// its backlog's link.
	DefaultLinkedGitlabProjectID *uuid.UUID
	// BaseBranch is optional; empty means "not set", and the task context
	// falls through to the backlog's.
	BaseBranch string
	// AllowedScope/ForbiddenScope are optional and fall through the same way.
	AllowedScope   string
	ForbiddenScope string
	// EstimatedPoints is optional: nil leaves the epic unestimated. Zero or
	// negative is rejected with ErrInvalidEstimate — an epic estimated at no
	// work and an epic nobody has estimated must stay distinguishable.
	EstimatedPoints *int
	// AssigneeUserID is optional: nil leaves the epic unassigned. A non-member
	// is rejected with ErrAssigneeNotMember.
	AssigneeUserID *uuid.UUID
}

// Create validates and creates an epic at the end of projectID's epic order.
// It returns ErrNotFound if projectID does not exist or the caller cannot see
// it.
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, p CreateParams) (Epic, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return Epic{}, err
	}

	fields, err := normalize(normalizeInput{
		Name:           p.Name,
		StartDate:      p.StartDate,
		DueOn:          p.DueOn,
		Priority:       p.Priority,
		Progress:       p.Progress,
		BaseBranch:     p.BaseBranch,
		AllowedScope:   p.AllowedScope,
		ForbiddenScope: p.ForbiddenScope,
	})
	if err != nil {
		return Epic{}, err
	}
	if err := validateEstimatedPoints(p.EstimatedPoints); err != nil {
		return Epic{}, err
	}
	if err := s.validateBacklog(ctx, ownerID, projectID, p.BacklogID); err != nil {
		return Epic{}, err
	}
	if err := s.validateLink(ctx, ownerID, projectID, p.DefaultLinkedGitlabProjectID); err != nil {
		return Epic{}, err
	}
	if p.AssigneeUserID != nil {
		if err := assignee.ValidateMember(ctx, s.q, projectID, *p.AssigneeUserID); err != nil {
			return Epic{}, err
		}
	}

	row, err := s.q.CreateEpic(ctx, db.CreateEpicParams{
		ProjectID:                    projectID,
		BacklogID:                    toUUID(p.BacklogID),
		Name:                         fields.Name,
		Description:                  p.Description,
		StartDate:                    toDate(p.StartDate),
		DueOn:                        toDate(p.DueOn),
		Priority:                     fields.Priority,
		Progress:                     fields.Progress,
		DefaultLinkedGitlabProjectID: toUUID(p.DefaultLinkedGitlabProjectID),
		BaseBranch:                   fields.BaseBranch,
		AllowedScope:                 fields.AllowedScope,
		ForbiddenScope:               fields.ForbiddenScope,
		EstimatedPoints:              toInt4(p.EstimatedPoints),
		AssigneeUserID:               toUUID(p.AssigneeUserID),
	})
	if err != nil {
		return Epic{}, fmt.Errorf("epic: create: %w", err)
	}
	e := fromRow(row)
	if err := s.attachAssigneeName(ctx, &e); err != nil {
		return Epic{}, err
	}
	return e, nil
}

// ListFilter narrows List to a subset of a project's epics. The zero value
// means "no filter, default (position) order".
type ListFilter struct {
	// BacklogID, when non-nil, only returns epics filed in that backlog;
	// BacklogUnfiled only those in no backlog at all. Mutually exclusive.
	BacklogID      *uuid.UUID
	BacklogUnfiled bool
	Priority       string // one of the Priority* constants, or "" (no filter)
	Progress       string // one of the Progress* constants, or "" (no filter)
	// AssigneeUserID/AssigneeUnassigned behave exactly as a backlog's: there
	// is no GitLab axis to OR against, since an epic has no GitLab
	// counterpart. Mutually exclusive.
	AssigneeUserID     *uuid.UUID
	AssigneeUnassigned bool
	// Sort is "" (position ASC, created_at ASC, the manual order),
	// SortPriority or SortProgress.
	Sort string
}

// List returns projectID's epics matching filter. It returns ErrNotFound if
// projectID does not exist or the caller cannot see it.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID, filter ListFilter) ([]Epic, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.q.ListEpicsByProject(ctx, db.ListEpicsByProjectParams{
		ProjectID:          projectID,
		BacklogID:          toUUID(filter.BacklogID),
		BacklogUnfiled:     filter.BacklogUnfiled,
		Priority:           filter.Priority,
		Progress:           filter.Progress,
		AssigneeUserID:     toUUID(filter.AssigneeUserID),
		AssigneeUnassigned: filter.AssigneeUnassigned,
		SortByPriority:     filter.Sort == SortPriority,
		SortByProgress:     filter.Sort == SortProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("epic: list: %w", err)
	}
	out := make([]Epic, len(rows))
	for i, row := range rows {
		out[i] = fromListRow(row)
	}
	if err := s.attachAssigneeNamesToList(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns the epic by ID, scoped through its project's members. It
// returns ErrNotFound both when the epic does not exist and when its project
// belongs to someone else.
func (s *Service) Get(ctx context.Context, ownerID, epicID uuid.UUID) (Epic, error) {
	row, err := s.q.GetEpicForOwner(ctx, db.GetEpicForOwnerParams{
		ID:          epicID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Epic{}, ErrNotFound
		}
		return Epic{}, fmt.Errorf("epic: get: %w", err)
	}
	e := fromRow(row)
	if err := s.attachAssigneeName(ctx, &e); err != nil {
		return Epic{}, err
	}
	return e, nil
}

// ProjectID returns the project epicID belongs to, with no owner check — only
// requireTokenResourceProject (internal/http) uses this, to compare against a
// bearer token's own project, which has no owner to join against.
func (s *Service) ProjectID(ctx context.Context, epicID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetEpicProjectID(ctx, epicID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("epic: project id: %w", err)
	}
	return projectID, nil
}

// UpdateParams are the attributes Update writes. Name and Description are
// always overwritten; everything else is Optional, so a caller
// that only renames an epic leaves the rest untouched rather than clearing
// it. An explicit null clears a nullable field; an explicit empty string
// resets a defaulted one.
type UpdateParams struct {
	// BacklogID absent keeps the epic where it is; an explicit null unfiles
	// it. Either way the epic's tasks follow it, in the same transaction.
	BacklogID   optional.Optional[*uuid.UUID]
	Name        string
	Description string
	StartDate   optional.Optional[*time.Time]
	DueOn       optional.Optional[*time.Time]
	// Priority absent keeps the current value; an explicit empty string resets
	// it to PriorityMedium.
	Priority optional.Optional[string]
	// Progress follows the same rule, resetting to ProgressNotStarted.
	Progress optional.Optional[string]
	// DefaultLinkedGitlabProjectID absent keeps the current value; an explicit
	// null falls the epic back to its backlog's link.
	DefaultLinkedGitlabProjectID optional.Optional[*uuid.UUID]
	// BaseBranch, AllowedScope and ForbiddenScope follow the
	// absent-keeps/empty-clears rule.
	BaseBranch     optional.Optional[string]
	AllowedScope   optional.Optional[string]
	ForbiddenScope optional.Optional[string]
	// EstimatedPoints absent keeps the current estimate; an explicit null
	// clears it back to "unestimated". Nothing clears it automatically: an
	// epic that has since been broken down keeps the number it was guessed at,
	// EffectivePoints simply stops consulting it.
	EstimatedPoints optional.Optional[*int]
	// AssigneeUserID absent keeps the current assignee; an explicit null
	// unassigns.
	AssigneeUserID optional.Optional[*uuid.UUID]
}

// Update overwrites name, description and position, and applies whichever of
// the optional fields the caller set. Ownership is enforced by the query, so a
// non-member gets ErrNotFound and nothing is written.
//
// When the epic moves to a different backlog, its tasks move with it in the
// same transaction: a task's backlog_id and its epic's must agree, or the task
// would report a backlog its own epic no longer belongs to.
func (s *Service) Update(ctx context.Context, ownerID, epicID uuid.UUID, p UpdateParams) (Epic, error) {
	// The UPDATE writes every column, so absent fields have to be resolved
	// against the stored row first. Get is viewer-scoped, so a foreign epic
	// stops here with ErrNotFound before anything is written; the
	// member-minimum write check happens once current.ProjectID is known.
	current, err := s.Get(ctx, ownerID, epicID)
	if err != nil {
		return Epic{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return Epic{}, err
	}

	fields, err := normalize(normalizeInput{
		Name:           p.Name,
		StartDate:      p.StartDate.Or(current.StartDate),
		DueOn:          p.DueOn.Or(current.DueOn),
		Priority:       p.Priority.Or(current.Priority),
		Progress:       p.Progress.Or(current.Progress),
		BaseBranch:     p.BaseBranch.Or(current.BaseBranch),
		AllowedScope:   p.AllowedScope.Or(current.AllowedScope),
		ForbiddenScope: p.ForbiddenScope.Or(current.ForbiddenScope),
	})
	if err != nil {
		return Epic{}, err
	}
	startDate := p.StartDate.Or(current.StartDate)
	dueOn := p.DueOn.Or(current.DueOn)
	estimatedPoints := p.EstimatedPoints.Or(current.EstimatedPoints)
	if _, changed := p.EstimatedPoints.Get(); changed {
		if err := validateEstimatedPoints(estimatedPoints); err != nil {
			return Epic{}, err
		}
	}

	// Only a value the caller actually sent is re-validated: the stored one
	// was checked when it was written, and a backlog/link since deleted is
	// already NULL here (ON DELETE SET NULL).
	backlogID := p.BacklogID.Or(current.BacklogID)
	if _, changed := p.BacklogID.Get(); changed {
		if err := s.validateBacklog(ctx, ownerID, current.ProjectID, backlogID); err != nil {
			return Epic{}, err
		}
	}
	link := p.DefaultLinkedGitlabProjectID.Or(current.DefaultLinkedGitlabProjectID)
	if _, changed := p.DefaultLinkedGitlabProjectID.Get(); changed {
		if err := s.validateLink(ctx, ownerID, current.ProjectID, link); err != nil {
			return Epic{}, err
		}
	}
	newAssignee := p.AssigneeUserID.Or(current.AssigneeUserID)
	if _, changed := p.AssigneeUserID.Get(); changed && newAssignee != nil {
		if err := assignee.ValidateMember(ctx, s.q, current.ProjectID, *newAssignee); err != nil {
			return Epic{}, err
		}
	}

	backlogChanged := !sameUUIDPtr(current.BacklogID, backlogID)

	var result Epic
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := q.UpdateEpicForOwner(ctx, db.UpdateEpicForOwnerParams{
			ID:                           epicID,
			OwnerUserID:                  ownerID,
			BacklogID:                    toUUID(backlogID),
			Name:                         fields.Name,
			Description:                  p.Description,
			StartDate:                    toDate(startDate),
			DueOn:                        toDate(dueOn),
			Priority:                     fields.Priority,
			Progress:                     fields.Progress,
			DefaultLinkedGitlabProjectID: toUUID(link),
			BaseBranch:                   fields.BaseBranch,
			AllowedScope:                 fields.AllowedScope,
			ForbiddenScope:               fields.ForbiddenScope,
			EstimatedPoints:              toInt4(estimatedPoints),
			AssigneeUserID:               toUUID(newAssignee),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("epic: update: %w", err)
		}
		result = fromRow(row)

		if backlogChanged {
			if err := q.MoveEpicTasksToBacklog(ctx, db.MoveEpicTasksToBacklogParams{
				EpicID:    toUUID(&epicID),
				BacklogID: toUUID(backlogID),
			}); err != nil {
				return fmt.Errorf("epic: update: move tasks: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Epic{}, err
	}
	if err := s.attachAssigneeName(ctx, &result); err != nil {
		return Epic{}, err
	}
	return result, nil
}

// SetTasks makes the epic's task set exactly taskIDs: every named task is
// filed under the epic (and moved to the epic's own backlog, since the two
// must agree), and every task currently in the epic that taskIDs no longer
// names is unfiled from it — keeping its backlog, exactly as deleting the
// epic would.
//
// It is declarative rather than an add/remove pair because that is what the
// screens actually hold: a picker showing which of a backlog's tasks belong
// to this epic. Both writes run in one transaction, so a set that names a
// task the caller can't have moves nothing at all.
//
// A duplicate id is harmless (the set is de-duplicated first); an id from
// another project, or one that doesn't exist, returns ErrTaskNotInProject.
func (s *Service) SetTasks(ctx context.Context, ownerID, epicID uuid.UUID, taskIDs []uuid.UUID) error {
	current, err := s.Get(ctx, ownerID, epicID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return err
	}

	unique := make([]uuid.UUID, 0, len(taskIDs))
	seen := make(map[uuid.UUID]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	// Checked before anything is written, so a foreign or missing id is
	// refused rather than rolled back. The row-count check inside the
	// transaction below stays as the guard against a task deleted or moved
	// out of the project between this check and the write.
	if len(unique) > 0 {
		count, err := s.q.CountTasksInProjectByIDs(ctx, db.CountTasksInProjectByIDsParams{
			ProjectID: current.ProjectID,
			TaskIds:   unique,
		})
		if err != nil {
			return fmt.Errorf("epic: set tasks: count: %w", err)
		}
		if count != int64(len(unique)) {
			return ErrTaskNotInProject
		}
	}

	return s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		if err := q.ClearEpicTasksExcept(ctx, db.ClearEpicTasksExceptParams{
			EpicID:  toUUID(&epicID),
			TaskIds: unique,
		}); err != nil {
			return fmt.Errorf("epic: set tasks: clear: %w", err)
		}
		if len(unique) == 0 {
			return nil
		}
		affected, err := q.AssignTasksToEpic(ctx, db.AssignTasksToEpicParams{
			EpicID:  epicID,
			TaskIds: unique,
		})
		if err != nil {
			return fmt.Errorf("epic: set tasks: assign: %w", err)
		}
		// The pre-check above already rejected foreign ids; a shortfall here
		// means one was deleted or moved out from under us in between, and
		// the transaction unwinds rather than applying the rest.
		if affected != int64(len(unique)) {
			return ErrTaskNotInProject
		}
		return nil
	})
}

// Delete removes the epic. Ownership is enforced by the query, so a non-member
// gets ErrNotFound and nothing is deleted. Tasks in the epic are not deleted:
// the schema's ON DELETE SET NULL drops them back to sitting directly in their
// backlog, which is exactly where they were before the epic existed.
func (s *Service) Delete(ctx context.Context, ownerID, epicID uuid.UUID) error {
	current, err := s.Get(ctx, ownerID, epicID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return err
	}
	affected, err := s.q.DeleteEpicForOwner(ctx, db.DeleteEpicForOwnerParams{
		ID:          epicID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		return fmt.Errorf("epic: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// EffectivePoints resolves how much work an epic represents, which is the one
// rule the two point sources have to be read through:
//
//   - once the epic has tasks, the sum of their sizes (taskPoints), because
//     that is the real breakdown and the estimate was only ever standing in
//     for it;
//   - while it has none, its pre-breakdown EstimatedPoints;
//   - with neither, unknown — ok=false, which callers must not flatten to 0.
//
// That last case is the bug issue #234 was filed for: internal/velocity's
// forecast counted only tasks, so an epic with no tasks weighed zero and a
// project with three refined-but-unbroken-down backlogs was told its remaining
// work was one period away. Zero and unknown are different answers, and only
// one of them is honest.
//
// taskPoints is nil when the epic has no tasks. The estimate is not consulted
// when it is non-nil, which is what keeps the same work from being counted
// twice — and why the estimate can be kept forever without ever contradicting
// the tasks.
func EffectivePoints(taskPoints, estimatedPoints *int) (int, bool) {
	switch {
	case taskPoints != nil:
		return *taskPoints, true
	case estimatedPoints != nil:
		return *estimatedPoints, true
	default:
		return 0, false
	}
}
