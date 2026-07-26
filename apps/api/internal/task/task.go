// Package task contains the task domain model and the service that creates
// and manages tasks inside a project's backlog. GitLab issue sync does not
// exist yet (see docs/plans/issue-sync.md); deleting a task is therefore
// trivially local-only today, but the rule holds regardless: delete must
// never reach out to GitLab, even once sync ships.
package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/project"
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
	ErrNotFound              = errors.New("task: not found")
	ErrBacklogNotInProject   = errors.New("task: backlog belongs to a different project")
	ErrAIContextFieldTooLong = errors.New("task: AI context fields must be at most 20000 characters")
)

// maxAIContextFieldLength bounds each of the four task_ai_contexts fields.
const maxAIContextFieldLength = 20000

const (
	StatusOpen   = "open"
	StatusClosed = "closed"
)

// GitlabInfo will carry the GitLab issue sync fields (sync_status,
// gitlab_web_url, last_error, ...) once GitLab connection sync ships
// (docs/plans/issue-sync.md phase 3+). It is always nil until then, so every
// task response already carries the "gitlab" key clients will need to read.
type GitlabInfo struct{}

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

func toInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

// CreateParams holds the fields accepted when creating a task. A nil
// BacklogID leaves the task unfiled (未分類).
type CreateParams struct {
	Title                  string
	Description            string
	BacklogID              *uuid.UUID
	AssigneeGitlabUserID   *int64
	AssigneeGitlabUsername string
	Labels                 []string
	DueOn                  *time.Time
}

// UpdateParams holds the fields accepted when updating a task. A nil
// BacklogID leaves the task unfiled (未分類). Status and ClosedAt are
// intentionally excluded: they only change through Close/Reopen, which keep
// the two fields consistent and idempotent.
type UpdateParams struct {
	Title                  string
	Description            string
	BacklogID              *uuid.UUID
	AssigneeGitlabUserID   *int64
	AssigneeGitlabUsername string
	Labels                 []string
	DueOn                  *time.Time
	Position               int32
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

// Service manages tasks inside projects owned by a single user.
type Service struct {
	q        db.Querier
	projects *project.Service
	backlogs *backlog.Service
}

// NewService constructs a task Service. projects and backlogs are used to
// verify project ownership and backlog membership before any write.
func NewService(q db.Querier, projects *project.Service, backlogs *backlog.Service) *Service {
	return &Service{q: q, projects: projects, backlogs: backlogs}
}

// normalizeTitle trims raw and enforces the 1-255 character rule.
func normalizeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(title); n < 1 || n > 255 {
		return "", ErrInvalidTitle
	}
	return title, nil
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
	if err := s.validateBacklog(ctx, ownerID, projectID, params.BacklogID); err != nil {
		return Task{}, err
	}

	row, err := s.q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID:              projectID,
		BacklogID:              toUUID(params.BacklogID),
		Title:                  title,
		Description:            params.Description,
		AssigneeGitlabUserID:   toInt8(params.AssigneeGitlabUserID),
		AssigneeGitlabUsername: params.AssigneeGitlabUsername,
		Labels:                 normalizeLabels(params.Labels),
		DueOn:                  toDate(params.DueOn),
		CreatedByUserID:        ownerID,
	})
	if err != nil {
		return Task{}, fmt.Errorf("task: create: %w", err)
	}
	return fromRow(row), nil
}

// ListFilter narrows List to a subset of a project's tasks. The zero value
// means "no filter": every task in the project, any status.
type ListFilter struct {
	BacklogID  *uuid.UUID // non-nil: only this backlog's tasks
	Unassigned bool       // true: only unfiled tasks (backlog_id IS NULL); mutually exclusive with BacklogID
	Status     string     // "open" | "closed" | "" (no filter)
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
		ProjectID:  projectID,
		Unassigned: filter.Unassigned,
		BacklogID:  toUUID(filter.BacklogID),
		Status:     filter.Status,
	})
	if err != nil {
		return nil, fmt.Errorf("task: list: %w", err)
	}
	out := make([]Task, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
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
	return fromRow(row), nil
}

// Update overwrites title, description, assignee, labels, due date, backlog
// and position. Ownership is enforced by the query, so a non-owner gets
// ErrNotFound and nothing is written.
func (s *Service) Update(ctx context.Context, ownerID, taskID uuid.UUID, params UpdateParams) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}

	title, err := normalizeTitle(params.Title)
	if err != nil {
		return Task{}, err
	}
	if err := s.validateBacklog(ctx, ownerID, current.ProjectID, params.BacklogID); err != nil {
		return Task{}, err
	}

	row, err := s.q.UpdateTaskForOwner(ctx, db.UpdateTaskForOwnerParams{
		ID:                     taskID,
		OwnerUserID:            ownerID,
		BacklogID:              toUUID(params.BacklogID),
		Title:                  title,
		Description:            params.Description,
		AssigneeGitlabUserID:   toInt8(params.AssigneeGitlabUserID),
		AssigneeGitlabUsername: params.AssigneeGitlabUsername,
		Labels:                 normalizeLabels(params.Labels),
		DueOn:                  toDate(params.DueOn),
		Position:               params.Position,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: update: %w", err)
	}
	return fromRow(row), nil
}

// AssignBacklog moves the task to backlogID, or back to unfiled (未分類)
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
// never moves the timestamp.
func (s *Service) Close(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if current.Status == StatusClosed {
		return current, nil
	}

	row, err := s.q.CloseTaskForOwner(ctx, db.CloseTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: close: %w", err)
	}
	return fromRow(row), nil
}

// Reopen marks the task open and clears closed_at. Reopening an
// already-open task is a no-op.
func (s *Service) Reopen(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if current.Status == StatusOpen {
		return current, nil
	}

	row, err := s.q.ReopenTaskForOwner(ctx, db.ReopenTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: reopen: %w", err)
	}
	return fromRow(row), nil
}

// Delete removes the task. It never touches GitLab: this issue predates any
// GitLab connection, and later sync work must keep that guarantee (deleting
// a task remembers nothing to resurrect on re-sync, per
// docs/plans/issue-sync.md). Ownership is enforced by the query, so a
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

	row, err := s.q.GetTaskAIContext(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AIContext{}, nil
		}
		return AIContext{}, fmt.Errorf("task: get ai context: %w", err)
	}
	return aiContextFromRow(row), nil
}
