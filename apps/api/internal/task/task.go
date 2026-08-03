// Package task contains the task domain model and the service that creates
// and manages tasks inside a project's backlog. Create/Update/Close/Reopen
// enqueue outbound GitLab issue sync jobs (internal/issuesync) in the same
// transaction as the task write, per docs/plans/issue-sync.md's "Outbound"
// section; Delete never does — deleting a task must never reach out to
// GitLab, so its link is simply forgotten (cascade), not resynced.
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/project"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is also returned when a task exists but belongs to
// another user (via its project), so callers never leak existence to
// non-owners.
var (
	ErrInvalidTitle          = errors.New("task: title must be 1-255 characters")
	ErrInvalidPriority       = errors.New("task: priority must be one of low, medium, high, urgent")
	ErrNotFound              = errors.New("task: not found")
	ErrBacklogNotInProject   = errors.New("task: backlog belongs to a different project")
	ErrAIContextFieldTooLong = errors.New("task: AI context fields must be at most 20000 characters")
	ErrSyncNotFailed         = errors.New("task: gitlab sync is not currently failed")
)

// maxAIContextFieldLength bounds each of the four task_ai_contexts fields.
const maxAIContextFieldLength = 20000

const (
	StatusOpen   = "open"
	StatusClosed = "closed"
)

// Priority values, app-only and never synced to GitLab (GitLab CE issues
// have no native priority field — priority label/weight is an EE feature),
// per the 000010 migration. SortPriority is the ListFilter.Sort value that
// switches List's ORDER BY from position to priority rank.
const (
	PriorityLow    = "low"
	PriorityMedium = "medium"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"

	SortPriority = "priority"
)

// Sort values accepted by ListForOwner's CrossProjectFilter.Sort, alongside
// SortPriority above (the cross-project list has no manual/DnD order to fall
// back to, so unlike ListFilter.Sort, "" is not itself a valid value —
// ListForOwner normalizes it to SortDueOn).
const (
	SortDueOn     = "dueOn"
	SortUpdatedAt = "updatedAt"
)

// Pagination bounds for ListForOwner, mirroring
// DefaultContextPerPage/MaxContextPerPage's role for ListContext.
const (
	DefaultCrossProjectLimit = 50
	MaxCrossProjectLimit     = 200
)

// GitLab sync status values, mirroring task_gitlab_links.sync_status
// (docs/plans/issue-sync.md). SyncStatusPending also covers a task whose
// issue.create job hasn't run yet — it has no task_gitlab_links row at all,
// so gitlabInfoForTask derives it from the job instead (see there).
const (
	SyncStatusSynced  = "synced"
	SyncStatusPending = "pending"
	SyncStatusFailed  = "failed"
)

// GitlabInfo carries a task's GitLab issue sync state: whether it pushed
// cleanly, is still in flight, or failed and why, plus enough to link to the
// issue itself. A nil *GitlabInfo (not this struct's zero value) means the
// task has never had a linked GitLab project to sync to at all — a purely
// local task, per docs/plans/issue-sync.md.
type GitlabInfo struct {
	SyncStatus   string     `json:"syncStatus"`
	LastError    string     `json:"lastError"`
	LastSyncedAt *time.Time `json:"lastSyncedAt"`
	IssueIID     *int64     `json:"issueIid"`
	WebURL       string     `json:"webUrl"`
}

// AIContext holds the app-only fields that describe a task for an AI agent:
// acceptance criteria, free-form context, and the allowed/forbidden change
// scope. These fields live in task_ai_contexts, never in tasks, and must
// never be sent to GitLab (see "Why the task is split across three tables"
// in docs/plans/issue-sync.md). A task with no task_ai_contexts row yet
// reports as the zero value rather than nil, so callers never need a nil
// check.
type AIContext struct {
	AcceptanceCriteria string     `json:"acceptanceCriteria"`
	AIContext          string     `json:"aiContext"`
	AllowedScope       string     `json:"allowedScope"`
	ForbiddenScope     string     `json:"forbiddenScope"`
	UpdatedAt          *time.Time `json:"updatedAt"`
}

// AIContextParams holds the fields accepted when upserting a task's AI
// context. Every field is optional (the empty string is valid).
type AIContextParams struct {
	AcceptanceCriteria string
	AIContext          string
	AllowedScope       string
	ForbiddenScope     string
}

func aiContextFromRow(row db.TaskAiContext) AIContext {
	return AIContext{
		AcceptanceCriteria: row.AcceptanceCriteria,
		AIContext:          row.AiContext,
		AllowedScope:       row.AllowedScope,
		ForbiddenScope:     row.ForbiddenScope,
		UpdatedAt:          timePtr(row.UpdatedAt),
	}
}

// validateAIContextParams enforces the length cap on each field. Unlike
// title, empty is always valid: acceptance criteria, AI context, and the
// allowed/forbidden scope are all optional.
func validateAIContextParams(params AIContextParams) error {
	fields := []string{params.AcceptanceCriteria, params.AIContext, params.AllowedScope, params.ForbiddenScope}
	for _, f := range fields {
		if utf8.RuneCountInString(f) > maxAIContextFieldLength {
			return ErrAIContextFieldTooLong
		}
	}
	return nil
}

// Task is the API-facing representation of a task.
type Task struct {
	ID                     uuid.UUID   `json:"id"`
	ProjectID              uuid.UUID   `json:"projectId"`
	BacklogID              *uuid.UUID  `json:"backlogId"`
	Title                  string      `json:"title"`
	Description            string      `json:"description"`
	Status                 string      `json:"status"`
	ClosedAt               *time.Time  `json:"closedAt"`
	AssigneeGitlabUserID   *int64      `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string      `json:"assigneeGitlabUsername"`
	Labels                 []string    `json:"labels"`
	DueOn                  *time.Time  `json:"dueOn"`
	StartDate              *time.Time  `json:"startDate"`
	Priority               string      `json:"priority"`
	Position               int32       `json:"position"`
	CreatedByUserID        uuid.UUID   `json:"createdByUserId"`
	CreatedAt              time.Time   `json:"createdAt"`
	UpdatedAt              time.Time   `json:"updatedAt"`
	Gitlab                 *GitlabInfo `json:"gitlab"`
	AIContext              AIContext   `json:"aiContext"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.Task) Task {
	return Task{
		ID:                     row.ID,
		ProjectID:              row.ProjectID,
		BacklogID:              uuidPtr(row.BacklogID),
		Title:                  row.Title,
		Description:            row.Description,
		Status:                 row.Status,
		ClosedAt:               timePtr(row.ClosedAt),
		AssigneeGitlabUserID:   int64Ptr(row.AssigneeGitlabUserID),
		AssigneeGitlabUsername: row.AssigneeGitlabUsername,
		Labels:                 row.Labels,
		DueOn:                  datePtr(row.DueOn),
		StartDate:              datePtr(row.StartDate),
		Priority:               row.Priority,
		Position:               row.Position,
		CreatedByUserID:        row.CreatedByUserID,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}
}

func uuidPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func datePtr(v pgtype.Date) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func int64Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func toUUID(v *uuid.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *v, Valid: true}
}

func toDate(v *time.Time) pgtype.Date {
	if v == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *v, Valid: true}
}

func toTimestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}

func toInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

// TaskWithProject is Task plus the name of the project it belongs to — the
// minimum extra context the cross-project task collection (GET
// /api/v1/tasks, issue #76) needs to be readable, since a task alone doesn't
// say which project it's in. Task is embedded rather than nested so its
// fields flatten into the same JSON object, the same way
// http.createAPITokenResponse embeds apitoken.APIToken.
//
// It intentionally carries no Gitlab sync state: ListForOwner is a single
// query across every owned project, and resolving sync state per task would
// turn that back into the N+1 lookup List already accepts for a single
// project's board view. A task's own single view (GET /tasks/{taskID}) is
// still the place to check that.
type TaskWithProject struct {
	Task
	ProjectName string `json:"projectName"`
}

// CreateParams holds the fields accepted when creating a task. A nil
// BacklogID leaves the task unfiled (Unclassified).
type CreateParams struct {
	Title                  string
	Description            string
	BacklogID              *uuid.UUID
	AssigneeGitlabUserID   *int64
	AssigneeGitlabUsername string
	Labels                 []string
	DueOn                  *time.Time
	StartDate              *time.Time
	// Priority defaults to PriorityMedium when empty.
	Priority string
}

// UpdateParams holds the fields accepted when updating a task. A nil
// BacklogID leaves the task unfiled (Unclassified). Status and ClosedAt are
// intentionally excluded: they only change through Close/Reopen, which keep
// the two fields consistent and idempotent.
// UpdateParams holds the fields accepted when updating a task. Every field
// is Optional: one left unset keeps its current value, so a client may PATCH
// a single attribute without having to echo back the rest of the task.
// Nullable fields (backlog, assignee ID, the two dates) are Optional[*T], so
// an explicit null clears them.
type UpdateParams struct {
	Title                  Optional[string]
	Description            Optional[string]
	BacklogID              Optional[*uuid.UUID]
	AssigneeGitlabUserID   Optional[*int64]
	AssigneeGitlabUsername Optional[string]
	Labels                 Optional[[]string]
	DueOn                  Optional[*time.Time]
	StartDate              Optional[*time.Time]
	// Priority left absent keeps the task's current priority; an explicit
	// empty string resets it to PriorityMedium, the same as an absent
	// Priority on CreateParams.
	Priority Optional[string]
	Position Optional[int32]
}

// resolvedUpdate is UpdateParams with every absent field filled in from the
// task's current values. Both the database write and the mirrored-field
// comparison work from it, so they can never disagree about what an absent
// field meant.
type resolvedUpdate struct {
	Title                  string
	Description            string
	BacklogID              *uuid.UUID
	AssigneeGitlabUserID   *int64
	AssigneeGitlabUsername string
	Labels                 []string
	DueOn                  *time.Time
	StartDate              *time.Time
	Priority               string
	Position               int32
}

func (p UpdateParams) resolve(current Task) resolvedUpdate {
	return resolvedUpdate{
		Title:                  p.Title.Or(current.Title),
		Description:            p.Description.Or(current.Description),
		BacklogID:              p.BacklogID.Or(current.BacklogID),
		AssigneeGitlabUserID:   p.AssigneeGitlabUserID.Or(current.AssigneeGitlabUserID),
		AssigneeGitlabUsername: p.AssigneeGitlabUsername.Or(current.AssigneeGitlabUsername),
		Labels:                 p.Labels.Or(current.Labels),
		DueOn:                  p.DueOn.Or(current.DueOn),
		StartDate:              p.StartDate.Or(current.StartDate),
		Priority:               p.Priority.Or(current.Priority),
		Position:               p.Position.Or(current.Position),
	}
}

// BuildGitlabIssuePayload converts a task's mirrored fields into the payload
// FlowLens will push to GitLab. It takes only a Task, which structurally
// cannot carry the task_ai_contexts fields (acceptance criteria, AI context,
// allowed/forbidden scope) — those live on AIContext, a separate type this
// function never accepts. This is the guarantee the sync feature is built
// against; see "Why the task is split across three tables" in
// docs/plans/issue-sync.md.
func BuildGitlabIssuePayload(t Task) gitlab.IssuePayload {
	var assigneeIDs []int64
	if t.AssigneeGitlabUserID != nil {
		assigneeIDs = []int64{*t.AssigneeGitlabUserID}
	}
	return gitlab.IssuePayload{
		Title:       t.Title,
		Description: t.Description,
		Labels:      t.Labels,
		DueDate:     t.DueOn,
		AssigneeIDs: assigneeIDs,
	}
}

// BuildGitlabUpdateIssuePayload is BuildGitlabIssuePayload for an
// already-linked task: the same mirrored fields (title, description,
// labels, due date, assignee), shaped for GitLab's partial-update issue
// endpoint. Every field is always sent — FlowLens has no notion of "this
// field didn't change" at push time, since a job's payload already carries
// the task's full current state (see internal/issuesync.UpdatePayload).
func BuildGitlabUpdateIssuePayload(t Task) gitlab.UpdateIssuePayload {
	var assigneeIDs []int64
	if t.AssigneeGitlabUserID != nil {
		assigneeIDs = []int64{*t.AssigneeGitlabUserID}
	}
	title := t.Title
	description := t.Description
	return gitlab.UpdateIssuePayload{
		Title:       &title,
		Description: &description,
		Labels:      t.Labels,
		DueDate:     t.DueOn,
		AssigneeIDs: assigneeIDs,
	}
}

// Service manages tasks inside projects owned by a single user.
type Service struct {
	q        db.Querier
	txRunner database.TxRunner
	projects *project.Service
	backlogs *backlog.Service
}

// NewService constructs a task Service. projects and backlogs are used to
// verify project ownership and backlog membership before any write.
// txRunner runs each write that must enqueue an outbound sync job (Create,
// Update, Close, Reopen) atomically with that enqueue, per
// docs/plans/issue-sync.md's "Outbound" section.
func NewService(q db.Querier, txRunner database.TxRunner, projects *project.Service, backlogs *backlog.Service) *Service {
	return &Service{q: q, txRunner: txRunner, projects: projects, backlogs: backlogs}
}

// normalizeTitle trims raw and enforces the 1-255 character rule.
func normalizeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(title); n < 1 || n > 255 {
		return "", ErrInvalidTitle
	}
	return title, nil
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

// validateBacklog checks that backlogID, if set, both belongs to ownerID and
// is inside projectID. A backlog that exists but belongs to a different
// project of the same owner is rejected the same as a missing one: it is
// simply not a valid destination for this task.
func (s *Service) validateBacklog(ctx context.Context, ownerID, projectID uuid.UUID, backlogID *uuid.UUID) error {
	if backlogID == nil {
		return nil
	}
	b, err := s.backlogs.Get(ctx, ownerID, *backlogID)
	if err != nil {
		if errors.Is(err, backlog.ErrNotFound) {
			return ErrBacklogNotInProject
		}
		return fmt.Errorf("task: validate backlog: %w", err)
	}
	if b.ProjectID != projectID {
		return ErrBacklogNotInProject
	}
	return nil
}

func normalizeLabels(labels []string) []string {
	if labels == nil {
		return []string{}
	}
	return labels
}

// Create validates title and backlog membership, then creates a task at the
// end of its backlog's (or the unfiled group's) position order. It returns
// ErrNotFound if projectID does not exist or belongs to another user.
//
// If projectID has a default linked GitLab project, an issue.create sync
// job is enqueued in the same transaction as the task write, so the task is
// always eventually pushed (docs/plans/issue-sync.md, "Outbound"); a
// project with no linked GitLab project yet creates a purely local task. An
// unspecified assignee defaults to the project's GitLab connection's own
// token identity — the MVP has one token per project, not per user
// (ADR-0008), so that token's account stands in for "the creator's GitLab
// account".
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, params CreateParams) (Task, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: create: %w", err)
	}

	title, err := normalizeTitle(params.Title)
	if err != nil {
		return Task{}, err
	}
	priority, err := normalizePriority(params.Priority)
	if err != nil {
		return Task{}, err
	}
	if err := s.validateBacklog(ctx, ownerID, projectID, params.BacklogID); err != nil {
		return Task{}, err
	}

	var result Task
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		assigneeID := params.AssigneeGitlabUserID
		assigneeUsername := params.AssigneeGitlabUsername

		defaultLink, hasLink, err := getDefaultLinkedGitlabProject(ctx, q, ownerID, projectID)
		if err != nil {
			return fmt.Errorf("task: create: %w", err)
		}
		if hasLink && assigneeID == nil {
			assigneeID, assigneeUsername, err = defaultAssignee(ctx, q, ownerID, projectID)
			if err != nil {
				return fmt.Errorf("task: create: %w", err)
			}
		}

		row, err := q.CreateTask(ctx, db.CreateTaskParams{
			ProjectID:              projectID,
			BacklogID:              toUUID(params.BacklogID),
			Title:                  title,
			Description:            params.Description,
			AssigneeGitlabUserID:   toInt8(assigneeID),
			AssigneeGitlabUsername: assigneeUsername,
			Labels:                 normalizeLabels(params.Labels),
			DueOn:                  toDate(params.DueOn),
			StartDate:              toDate(params.StartDate),
			Priority:               priority,
			CreatedByUserID:        ownerID,
		})
		if err != nil {
			return fmt.Errorf("task: create: %w", err)
		}
		result = fromRow(row)

		if !hasLink {
			return nil
		}
		payload, err := json.Marshal(issuesync.CreatePayload{
			LinkedGitlabProjectID: defaultLink.ID,
			IssuePayload:          BuildGitlabIssuePayload(result),
		})
		if err != nil {
			return fmt.Errorf("task: create: encode sync payload: %w", err)
		}
		taskID := result.ID
		if _, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
			ProjectID: projectID,
			TaskID:    &taskID,
			Kind:      issuesync.KindIssueCreate,
			Payload:   payload,
		}); err != nil {
			return fmt.Errorf("task: create: enqueue sync job: %w", err)
		}
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return result, nil
}

// getDefaultLinkedGitlabProject looks up projectID's default linked GitLab
// project, if any, scoped to ownerID like every other task query. A project
// with no linked GitLab project yet is not an error: the task is created as
// a purely local one.
func getDefaultLinkedGitlabProject(ctx context.Context, q db.Querier, ownerID, projectID uuid.UUID) (db.LinkedGitlabProject, bool, error) {
	link, err := q.GetDefaultLinkedGitlabProjectForOwner(ctx, db.GetDefaultLinkedGitlabProjectForOwnerParams{
		ID:          projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.LinkedGitlabProject{}, false, nil
		}
		return db.LinkedGitlabProject{}, false, err
	}
	return link, true, nil
}

// defaultAssignee resolves the GitLab identity a newly created task is
// assigned to when the caller does not specify one, per Create's doc
// comment. A project with no GitLab connection, or a connection whose token
// identity is not yet known, defaults to no assignee.
func defaultAssignee(ctx context.Context, q db.Querier, ownerID, projectID uuid.UUID) (*int64, string, error) {
	conn, err := q.GetGitlabConnectionForOwner(ctx, db.GetGitlabConnectionForOwnerParams{
		ProjectID:   projectID,
		OwnerUserID: ownerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", err
	}
	if !conn.TokenGitlabUserID.Valid {
		return nil, "", nil
	}
	id := conn.TokenGitlabUserID.Int64
	return &id, conn.TokenGitlabUsername, nil
}

// ListFilter narrows List to a subset of a project's tasks. The zero value
// means "no filter": every task in the project, any status.
type ListFilter struct {
	BacklogID  *uuid.UUID // non-nil: only this backlog's tasks
	Unassigned bool       // true: only unfiled tasks (backlog_id IS NULL); mutually exclusive with BacklogID
	Status     string     // "open" | "closed" | "" (no filter)
	Priority   string     // one of the Priority* constants, or "" (no filter)
	// Sort is "" (position ASC, created_at ASC, the manual/DnD order) or
	// SortPriority (priority rank DESC, then the same tiebreak).
	Sort string
}

// List returns projectID's tasks matching filter, ordered by position. It
// returns ErrNotFound if projectID does not exist or belongs to another
// user.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID, filter ListFilter) ([]Task, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("task: list: %w", err)
	}

	rows, err := s.q.ListTasksByProject(ctx, db.ListTasksByProjectParams{
		ProjectID:      projectID,
		Unassigned:     filter.Unassigned,
		BacklogID:      toUUID(filter.BacklogID),
		Status:         filter.Status,
		Priority:       filter.Priority,
		SortByPriority: filter.Sort == SortPriority,
	})
	if err != nil {
		return nil, fmt.Errorf("task: list: %w", err)
	}
	out := make([]Task, len(rows))
	for i, row := range rows {
		t := fromRow(row)
		info, err := s.gitlabInfoForTask(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		t.Gitlab = info
		out[i] = t
	}
	return out, nil
}

// CrossProjectFilter narrows ListForOwner to a subset of every task the
// caller owns across every project. Every field's zero value means "no
// filter", the same convention ListFilter uses, except Sort and Limit, which
// ListForOwner defaults rather than leaves off (there is no manual/DnD order
// to fall back to, and an unbounded cross-project scan has no natural cap).
type CrossProjectFilter struct {
	Status        string      // "open" | "closed" | "" (no filter)
	Priority      string      // one of the Priority* constants, or "" (no filter)
	DueBefore     *time.Time  // non-nil: only tasks due on or before this date
	DueAfter      *time.Time  // non-nil: only tasks due on or after this date
	StartedBefore *time.Time  // non-nil: only tasks whose start date has arrived (start_date <= this date)
	ProjectIDs    []uuid.UUID // non-empty: only these projects, still scoped to the caller's own
	// Sort is one of SortDueOn (default), SortPriority or SortUpdatedAt.
	Sort string
	// Limit caps the number of tasks returned; non-positive defaults to
	// DefaultCrossProjectLimit, and anything above MaxCrossProjectLimit is
	// capped to it.
	Limit int
}

// ListForOwner returns every task across every project ownerID owns, per
// filter, for the cross-project task collection (GET /api/v1/tasks, issue
// #76) — "what should I be doing right now" without opening each project in
// turn. Unlike every other method here, it takes no projectID: there is
// nothing further to scope by ownerID with, since it already spans every
// project that owner has.
func (s *Service) ListForOwner(ctx context.Context, ownerID uuid.UUID, filter CrossProjectFilter) ([]TaskWithProject, error) {
	sortBy := filter.Sort
	if sortBy == "" {
		sortBy = SortDueOn
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultCrossProjectLimit
	}
	if limit > MaxCrossProjectLimit {
		limit = MaxCrossProjectLimit
	}
	projectIDs := filter.ProjectIDs
	if projectIDs == nil {
		projectIDs = []uuid.UUID{}
	}

	rows, err := s.q.ListTasksForOwner(ctx, db.ListTasksForOwnerParams{
		OwnerUserID:   ownerID,
		Status:        filter.Status,
		Priority:      filter.Priority,
		DueBefore:     toDate(filter.DueBefore),
		DueAfter:      toDate(filter.DueAfter),
		StartedBefore: toDate(filter.StartedBefore),
		ProjectIds:    projectIDs,
		Sort:          sortBy,
		LimitCount:    int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("task: list for owner: %w", err)
	}
	out := make([]TaskWithProject, len(rows))
	for i, row := range rows {
		out[i] = fromCrossProjectRow(row)
	}
	return out, nil
}

// fromCrossProjectRow maps a ListTasksForOwner row to TaskWithProject. It is
// separate from fromRow because db.ListTasksForOwnerRow is its own sqlc type
// (the joined project_name column keeps it from matching db.Task), not
// because the two ever disagree about a shared field.
func fromCrossProjectRow(row db.ListTasksForOwnerRow) TaskWithProject {
	return TaskWithProject{
		Task: fromRow(db.Task{
			ID:                     row.ID,
			ProjectID:              row.ProjectID,
			BacklogID:              row.BacklogID,
			Title:                  row.Title,
			Description:            row.Description,
			Status:                 row.Status,
			ClosedAt:               row.ClosedAt,
			AssigneeGitlabUserID:   row.AssigneeGitlabUserID,
			AssigneeGitlabUsername: row.AssigneeGitlabUsername,
			Labels:                 row.Labels,
			DueOn:                  row.DueOn,
			StartDate:              row.StartDate,
			Priority:               row.Priority,
			Position:               row.Position,
			CreatedByUserID:        row.CreatedByUserID,
			CreatedAt:              row.CreatedAt,
			UpdatedAt:              row.UpdatedAt,
		}),
		ProjectName: row.ProjectName,
	}
}

// Get returns the task by ID, scoped through its project's owner. It
// returns ErrNotFound both when the task does not exist and when its
// project belongs to another user.
func (s *Service) Get(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	row, err := s.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: get: %w", err)
	}
	t := fromRow(row)
	info, err := s.gitlabInfoForTask(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	t.Gitlab = info
	return t, nil
}

// ProjectID returns the project taskID belongs to, with no owner check —
// only requireTokenResourceProject (internal/http, issue #66) uses this, to
// compare against a bearer token's own project. Every other method on this
// Service already scopes by owner; this one exists because a token has no
// owner to join against in the first place.
func (s *Service) ProjectID(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetTaskProjectID(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("task: project id: %w", err)
	}
	return projectID, nil
}

// gitlabInfoFromLink builds a task's GitlabInfo from its task_gitlab_links
// row: the common case once a task has ever successfully pushed to GitLab.
func gitlabInfoFromLink(link db.TaskGitlabLink) *GitlabInfo {
	issueIID := link.GitlabIssueIid
	return &GitlabInfo{
		SyncStatus:   link.SyncStatus,
		LastError:    link.LastError,
		LastSyncedAt: timePtr(link.LastSyncedAt),
		IssueIID:     &issueIID,
		WebURL:       link.GitlabWebUrl,
	}
}

// gitlabInfoForTask resolves taskID's GitlabInfo for API responses. A task
// with a task_gitlab_links row reads its sync state directly from there. A
// task with none yet falls back to syncStatusFromLatestJob. A task with
// neither a link nor any sync job never had a linked GitLab project to push
// to, and reports as nil (untracked, purely local, per
// docs/plans/issue-sync.md).
func (s *Service) gitlabInfoForTask(ctx context.Context, taskID uuid.UUID) (*GitlabInfo, error) {
	link, err := s.q.GetTaskGitlabLinkByTaskID(ctx, taskID)
	if err == nil {
		return gitlabInfoFromLink(link), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("task: get gitlab link: %w", err)
	}

	status, lastError, found, err := s.syncStatusFromLatestJob(ctx, taskID)
	if err != nil || !found {
		return nil, err
	}
	return &GitlabInfo{SyncStatus: status, LastError: lastError}, nil
}

// syncStatusFromLatestJob derives a task's sync status and error from its
// most recent sync job, for a task with no task_gitlab_links row yet — its
// issue.create job hasn't succeeded. Still pending/running reports as
// SyncStatusPending, permanently failed reports as SyncStatusFailed with the
// job's error. found is false when there is no job at all, or when the
// job's own status is anything else ("succeeded" without a link row is
// unexpected — HandleIssueCreate always creates the link before reporting
// success — so it is treated the same as "no job" rather than guessed at).
func (s *Service) syncStatusFromLatestJob(ctx context.Context, taskID uuid.UUID) (status, lastError string, found bool, err error) {
	job, err := s.q.GetLatestSyncJobForTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("task: get latest sync job: %w", err)
	}
	switch job.Status {
	case "pending", "running":
		return SyncStatusPending, "", true, nil
	case "failed":
		return SyncStatusFailed, job.LastError, true, nil
	default:
		return "", "", false, nil
	}
}

// gitlabContextForTask is gitlabInfoForTask for the AI-facing /context
// endpoints: the same sync-state derivation, plus the linked GitLab
// project's path when a link exists (GitlabContext, not GitlabInfo — see
// Context's doc comment for why the two are separate types).
func (s *Service) gitlabContextForTask(ctx context.Context, taskID uuid.UUID) (*GitlabContext, error) {
	link, err := s.q.GetTaskGitlabLinkWithProjectPathByTaskID(ctx, taskID)
	if err == nil {
		issueIID := link.GitlabIssueIid
		return &GitlabContext{
			SyncStatus:   link.SyncStatus,
			LastError:    link.LastError,
			LastSyncedAt: timePtr(link.LastSyncedAt),
			IssueIID:     &issueIID,
			WebURL:       link.GitlabWebUrl,
			ProjectPath:  link.PathWithNamespace,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("task: get gitlab link with project path: %w", err)
	}

	status, lastError, found, err := s.syncStatusFromLatestJob(ctx, taskID)
	if err != nil || !found {
		return nil, err
	}
	return &GitlabContext{SyncStatus: status, LastError: lastError}, nil
}

// RetrySync re-enqueues a task's most recent failed GitLab push. It returns
// ErrSyncNotFailed (mapped to 409 by the HTTP layer) unless the task's
// current gitlab.syncStatus is SyncStatusFailed — the check the UI itself
// uses to decide whether to show the retry button, so the two can never
// disagree. On success, the task's link (if any) is put back to 'pending'
// immediately, and its job is reset to run again with a fresh attempt
// budget, so the outbox worker picks it up on its next poll.
func (s *Service) RetrySync(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	if _, err := s.Get(ctx, ownerID, taskID); err != nil {
		return Task{}, err
	}
	info, err := s.gitlabInfoForTask(ctx, taskID)
	if err != nil {
		return Task{}, fmt.Errorf("task: retry sync: %w", err)
	}
	if info == nil || info.SyncStatus != SyncStatusFailed {
		return Task{}, ErrSyncNotFailed
	}

	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		if _, err := q.RetryFailedSyncJobForTask(ctx, taskID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSyncNotFailed
			}
			return fmt.Errorf("task: retry sync: reset job: %w", err)
		}
		if _, err := q.MarkTaskGitlabLinkPendingForTask(ctx, taskID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("task: retry sync: mark link pending: %w", err)
		}
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, ownerID, taskID)
}

// Update overwrites title, description, assignee, labels, due date, backlog
// and position. Ownership is enforced by the query, so a non-owner gets
// ErrNotFound and nothing is written.
//
// If title, description, assignee, labels or due date actually change and
// the task already has a GitLab link, an issue.update sync job is enqueued
// in the same transaction, deduped per task so rapid repeated edits
// collapse into one pending push (docs/plans/issue-sync.md, "Outbound"). A
// backlog/position-only edit, or a task with no link, enqueues nothing.
func (s *Service) Update(ctx context.Context, ownerID, taskID uuid.UUID, params UpdateParams) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}

	resolved := params.resolve(current)

	title, err := normalizeTitle(resolved.Title)
	if err != nil {
		return Task{}, err
	}
	resolved.Title = title
	priority, err := normalizePriority(resolved.Priority)
	if err != nil {
		return Task{}, err
	}
	resolved.Priority = priority
	if err := s.validateBacklog(ctx, ownerID, current.ProjectID, resolved.BacklogID); err != nil {
		return Task{}, err
	}

	mirroredChanged := mirroredFieldsChanged(current, resolved)

	var result Task
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := q.UpdateTaskForOwner(ctx, db.UpdateTaskForOwnerParams{
			ID:                     taskID,
			OwnerUserID:            ownerID,
			BacklogID:              toUUID(resolved.BacklogID),
			Title:                  resolved.Title,
			Description:            resolved.Description,
			AssigneeGitlabUserID:   toInt8(resolved.AssigneeGitlabUserID),
			AssigneeGitlabUsername: resolved.AssigneeGitlabUsername,
			Labels:                 normalizeLabels(resolved.Labels),
			DueOn:                  toDate(resolved.DueOn),
			StartDate:              toDate(resolved.StartDate),
			Priority:               resolved.Priority,
			Position:               resolved.Position,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("task: update: %w", err)
		}
		result = fromRow(row)

		if !mirroredChanged {
			return nil
		}
		payload, err := json.Marshal(issuesync.UpdatePayload{UpdateIssuePayload: BuildGitlabUpdateIssuePayload(result)})
		if err != nil {
			return fmt.Errorf("task: update: encode sync payload: %w", err)
		}
		return enqueueIfLinked(ctx, q, taskID, result.ProjectID, issuesync.KindIssueUpdate,
			fmt.Sprintf("issue.update:%s", taskID), payload)
	})
	if err != nil {
		return Task{}, err
	}
	return result, nil
}

// mirroredFieldsChanged reports whether any of the fields FlowLens mirrors
// to GitLab (title, description, assignee, labels, due date) differ between
// current and the resolved update — the trigger for enqueueing issue.update.
// Backlog, position and start date are app-only and never compared here, so
// a PATCH touching only those enqueues nothing.
func mirroredFieldsChanged(current Task, resolved resolvedUpdate) bool {
	if current.Title != resolved.Title || current.Description != resolved.Description {
		return true
	}
	if !equalInt64Ptr(current.AssigneeGitlabUserID, resolved.AssigneeGitlabUserID) {
		return true
	}
	if current.AssigneeGitlabUsername != resolved.AssigneeGitlabUsername {
		return true
	}
	if !slices.Equal(current.Labels, normalizeLabels(resolved.Labels)) {
		return true
	}
	if !equalTimePtr(current.DueOn, resolved.DueOn) {
		return true
	}
	return false
}

func equalInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func equalTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// enqueueIfLinked enqueues a sync job for taskID only if it already has a
// GitLab link — a task with none is purely local
// (docs/plans/issue-sync.md), so there is nothing to push. payload may be
// nil (issue.close/issue.reopen carry no payload).
func enqueueIfLinked(ctx context.Context, q db.Querier, taskID, projectID uuid.UUID, kind, dedupeKey string, payload []byte) error {
	if _, err := q.GetTaskGitlabLinkByTaskID(ctx, taskID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("task: check gitlab link: %w", err)
	}
	if _, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: projectID,
		TaskID:    &taskID,
		Kind:      kind,
		Payload:   payload,
		DedupeKey: dedupeKey,
	}); err != nil {
		return fmt.Errorf("task: enqueue sync job: %w", err)
	}
	return nil
}

// AssignBacklog moves the task to backlogID, or back to unfiled (Unclassified)
// when backlogID is nil. It returns ErrBacklogNotInProject if backlogID
// belongs to a different project than the task's.
func (s *Service) AssignBacklog(ctx context.Context, ownerID, taskID uuid.UUID, backlogID *uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if err := s.validateBacklog(ctx, ownerID, current.ProjectID, backlogID); err != nil {
		return Task{}, err
	}

	row, err := s.q.AssignTaskBacklogForOwner(ctx, db.AssignTaskBacklogForOwnerParams{
		ID:          taskID,
		OwnerUserID: ownerID,
		BacklogID:   toUUID(backlogID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: assign backlog: %w", err)
	}
	return fromRow(row), nil
}

// Close marks the task closed and stamps closed_at. Closing an
// already-closed task is a no-op: closed_at is left untouched so re-closing
// never moves the timestamp, and — since nothing changed — no sync job is
// enqueued either. Otherwise, a linked task enqueues issue.close in the
// same transaction as the status write.
func (s *Service) Close(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if current.Status == StatusClosed {
		return current, nil
	}

	var result Task
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := q.CloseTaskForOwner(ctx, db.CloseTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("task: close: %w", err)
		}
		result = fromRow(row)
		return enqueueIfLinked(ctx, q, taskID, result.ProjectID, issuesync.KindIssueClose, "", nil)
	})
	if err != nil {
		return Task{}, err
	}
	return result, nil
}

// Reopen marks the task open and clears closed_at. Reopening an
// already-open task is a no-op, the same as Close. Otherwise, a linked task
// enqueues issue.reopen in the same transaction as the status write.
func (s *Service) Reopen(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if current.Status == StatusOpen {
		return current, nil
	}

	var result Task
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := q.ReopenTaskForOwner(ctx, db.ReopenTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("task: reopen: %w", err)
		}
		result = fromRow(row)
		return enqueueIfLinked(ctx, q, taskID, result.ProjectID, issuesync.KindIssueReopen, "", nil)
	})
	if err != nil {
		return Task{}, err
	}
	return result, nil
}

// Delete removes the task and never touches GitLab, even for a linked task:
// the ON DELETE CASCADE on task_gitlab_links/sync_jobs simply forgets the
// link, so a later re-sync does not resurrect it, per
// docs/plans/issue-sync.md. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted.
func (s *Service) Delete(ctx context.Context, ownerID, taskID uuid.UUID) error {
	affected, err := s.q.DeleteTaskForOwner(ctx, db.DeleteTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		return fmt.Errorf("task: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertAIContext creates or overwrites taskID's AI context in one call: the
// first call creates the task_ai_contexts row, later calls fully overwrite
// it. It never touches the task row itself, so tasks.updated_at is left
// untouched and no sync job is ever triggered by an AI-context-only edit.
// Ownership is enforced via Get, so a non-owner gets ErrNotFound and nothing
// is written.
func (s *Service) UpsertAIContext(ctx context.Context, ownerID, taskID uuid.UUID, params AIContextParams) (AIContext, error) {
	if _, err := s.Get(ctx, ownerID, taskID); err != nil {
		return AIContext{}, err
	}
	if err := validateAIContextParams(params); err != nil {
		return AIContext{}, err
	}

	row, err := s.q.UpsertTaskAIContext(ctx, db.UpsertTaskAIContextParams{
		TaskID:             taskID,
		AcceptanceCriteria: params.AcceptanceCriteria,
		AiContext:          params.AIContext,
		AllowedScope:       params.AllowedScope,
		ForbiddenScope:     params.ForbiddenScope,
	})
	if err != nil {
		return AIContext{}, fmt.Errorf("task: upsert ai context: %w", err)
	}
	return aiContextFromRow(row), nil
}

// GetAIContext returns taskID's AI context, scoped through its project's
// owner like Get. A task with no task_ai_contexts row yet (no AI context set
// so far) returns the zero value, not an error.
func (s *Service) GetAIContext(ctx context.Context, ownerID, taskID uuid.UUID) (AIContext, error) {
	if _, err := s.Get(ctx, ownerID, taskID); err != nil {
		return AIContext{}, err
	}
	return s.aiContextForTask(ctx, taskID)
}

// aiContextForTask resolves taskID's AI context with no ownership check of
// its own — callers must have already scoped taskID (via Get or
// GetTaskForProject). A task with no task_ai_contexts row yet returns the
// zero value, not an error.
func (s *Service) aiContextForTask(ctx context.Context, taskID uuid.UUID) (AIContext, error) {
	row, err := s.q.GetTaskAIContext(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AIContext{}, nil
		}
		return AIContext{}, fmt.Errorf("task: get ai context: %w", err)
	}
	return aiContextFromRow(row), nil
}

// Pagination defaults and bounds for ListContext/ListContextForProject,
// mirroring internal/webhookevent's.
const (
	DefaultContextPerPage = 20
	MaxContextPerPage     = 100
)

// GitlabContext is GitlabInfo's AI-facing counterpart: the same sync state,
// plus the linked GitLab project's path so an AI agent can identify the
// issue without a second call. See Context's doc comment for why this is a
// distinct type from GitlabInfo rather than an added field on it.
type GitlabContext struct {
	SyncStatus   string     `json:"syncStatus"`
	LastError    string     `json:"lastError"`
	LastSyncedAt *time.Time `json:"lastSyncedAt"`
	IssueIID     *int64     `json:"issueIid"`
	WebURL       string     `json:"webUrl"`
	ProjectPath  string     `json:"projectPath"`
}

// Context is the stable, documented response shape for the AI-facing
// endpoints (GET /api/v1/tasks/{taskID}/context and GET
// /api/v1/projects/{projectID}/tasks/context — docs/plans/issue-sync.md,
// "AI-facing"): a task's mirrored fields, its GitLab sync state including
// project path, and its AI context, all in one payload so an AI agent needs
// exactly one call. It is defined separately from Task, whose JSON shape
// backs the UI's own CRUD endpoints and may evolve independently — an
// external AI integration parses this contract, so its field names and
// nesting must stay stable regardless of what Task does.
type Context struct {
	ID                     uuid.UUID      `json:"id"`
	ProjectID              uuid.UUID      `json:"projectId"`
	BacklogID              *uuid.UUID     `json:"backlogId"`
	Title                  string         `json:"title"`
	Description            string         `json:"description"`
	Status                 string         `json:"status"`
	AssigneeGitlabUserID   *int64         `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string         `json:"assigneeGitlabUsername"`
	Labels                 []string       `json:"labels"`
	DueOn                  *time.Time     `json:"dueOn"`
	UpdatedAt              time.Time      `json:"updatedAt"`
	Gitlab                 *GitlabContext `json:"gitlab"`
	AcceptanceCriteria     string         `json:"acceptanceCriteria"`
	AIContext              string         `json:"aiContext"`
	AllowedScope           string         `json:"allowedScope"`
	ForbiddenScope         string         `json:"forbiddenScope"`
}

// toContext combines t with its already-resolved GitLab and AI context into
// the AI-facing Context shape.
func toContext(t Task, gc *GitlabContext, ai AIContext) Context {
	return Context{
		ID:                     t.ID,
		ProjectID:              t.ProjectID,
		BacklogID:              t.BacklogID,
		Title:                  t.Title,
		Description:            t.Description,
		Status:                 t.Status,
		AssigneeGitlabUserID:   t.AssigneeGitlabUserID,
		AssigneeGitlabUsername: t.AssigneeGitlabUsername,
		Labels:                 t.Labels,
		DueOn:                  t.DueOn,
		UpdatedAt:              t.UpdatedAt,
		Gitlab:                 gc,
		AcceptanceCriteria:     ai.AcceptanceCriteria,
		AIContext:              ai.AIContext,
		AllowedScope:           ai.AllowedScope,
		ForbiddenScope:         ai.ForbiddenScope,
	}
}

// Context returns taskID's combined AI-facing context for a
// session-authenticated caller, scoped through its project's owner like Get.
func (s *Service) Context(ctx context.Context, ownerID, taskID uuid.UUID) (Context, error) {
	t, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Context{}, err
	}
	return s.contextForTask(ctx, t)
}

// ContextForProject is Context for a bearer-token caller already scoped to
// a single project (internal/apitoken): taskID must belong to projectID or
// this reports ErrNotFound, the same way Get reports ErrNotFound for a
// foreign owner. There is no owner to check — the token's project is
// authorization enough (see internal/http.requireBearerAuth).
func (s *Service) ContextForProject(ctx context.Context, projectID, taskID uuid.UUID) (Context, error) {
	row, err := s.q.GetTaskForProject(ctx, db.GetTaskForProjectParams{ID: taskID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Context{}, ErrNotFound
		}
		return Context{}, fmt.Errorf("task: context for project: %w", err)
	}
	return s.contextForTask(ctx, fromRow(row))
}

// contextForTask resolves t's GitLab and AI context and combines them with
// t. It performs no authorization of its own — callers (Context,
// ContextForProject, listContext) must have already scoped t.
func (s *Service) contextForTask(ctx context.Context, t Task) (Context, error) {
	gc, err := s.gitlabContextForTask(ctx, t.ID)
	if err != nil {
		return Context{}, err
	}
	ai, err := s.aiContextForTask(ctx, t.ID)
	if err != nil {
		return Context{}, err
	}
	return toContext(t, gc, ai), nil
}

// ContextListFilter narrows ListContext/ListContextForProject to a subset of
// a project's tasks, plus paging. The zero value means "no filter, first
// page, default page size".
type ContextListFilter struct {
	BacklogID    *uuid.UUID // non-nil: only this backlog's tasks
	Status       string     // "open" | "closed" | "" (no filter)
	UpdatedSince *time.Time // non-nil: only tasks updated at or after this time
	Page         int
	PerPage      int
}

// ContextPage is one page of ListContext/ListContextForProject's results,
// ordered the same way List is (position ASC, created_at ASC).
type ContextPage struct {
	Tasks    []Context
	NextPage int // 0 when there is no next page
}

// ListContext returns a page of projectID's combined AI-facing task
// contexts for a session-authenticated caller, scoped through its project's
// owner like List. It returns ErrNotFound if projectID does not exist or
// belongs to another user.
func (s *Service) ListContext(ctx context.Context, ownerID, projectID uuid.UUID, filter ContextListFilter) (ContextPage, error) {
	if _, err := s.projects.Get(ctx, ownerID, projectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return ContextPage{}, ErrNotFound
		}
		return ContextPage{}, fmt.Errorf("task: list context: %w", err)
	}
	return s.listContext(ctx, projectID, filter)
}

// ListContextForProject is ListContext for a bearer-token caller already
// scoped to a single project by internal/http.requireTokenProjectMatch — no
// additional ownership check.
func (s *Service) ListContextForProject(ctx context.Context, projectID uuid.UUID, filter ContextListFilter) (ContextPage, error) {
	return s.listContext(ctx, projectID, filter)
}

// listContext is ListContext/ListContextForProject once projectID is known
// to be valid for the caller. It fetches one extra row per page to detect
// whether another page follows, the same convention
// internal/webhookevent.Service.List uses.
func (s *Service) listContext(ctx context.Context, projectID uuid.UUID, filter ContextListFilter) (ContextPage, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = DefaultContextPerPage
	}
	if perPage > MaxContextPerPage {
		perPage = MaxContextPerPage
	}

	rows, err := s.q.ListTasksByProjectPaged(ctx, db.ListTasksByProjectPagedParams{
		ProjectID:    projectID,
		BacklogID:    toUUID(filter.BacklogID),
		Status:       filter.Status,
		UpdatedSince: toTimestamptz(filter.UpdatedSince),
		LimitCount:   int32(perPage + 1),
		OffsetCount:  int32((page - 1) * perPage),
	})
	if err != nil {
		return ContextPage{}, fmt.Errorf("task: list context: %w", err)
	}

	nextPage := 0
	if len(rows) > perPage {
		rows = rows[:perPage]
		nextPage = page + 1
	}

	tasks := make([]Context, len(rows))
	for i, row := range rows {
		c, err := s.contextForTask(ctx, fromRow(row))
		if err != nil {
			return ContextPage{}, err
		}
		tasks[i] = c
	}
	return ContextPage{Tasks: tasks, NextPage: nextPage}, nil
}
