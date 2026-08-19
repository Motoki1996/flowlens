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
	ErrInvalidProgress       = errors.New("task: progress must be one of not_started, in_progress, on_hold, done")
	ErrInvalidSize           = errors.New("task: size must be one of xs, s, m, l, xl")
	ErrNotFound              = errors.New("task: not found")
	ErrBacklogNotInProject   = errors.New("task: backlog belongs to a different project")
	ErrAIContextFieldTooLong = errors.New("task: AI context fields must be at most 20000 characters")
	ErrSyncNotFailed         = errors.New("task: gitlab sync is not currently failed")
	ErrTaskIDsMismatch       = errors.New("task: taskIds must exactly match the current tasks in that backlog")
	ErrForbidden             = errors.New("task: forbidden")
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

// Progress values, the task's own four-stage work state, app-only and never
// synced to GitLab per the 000011 migration. It is deliberately separate from
// Status: that is the GitLab issue state (open/closed) and syncs both ways,
// while progress is FlowLens's own and neither value ever writes the other —
// a task closed on GitLab keeps whatever progress it had. SortProgress is the
// Sort value that orders by progress rank, running not_started first through
// done, so the order reads as the work advancing.
const (
	ProgressNotStarted = "not_started"
	ProgressInProgress = "in_progress"
	ProgressOnHold     = "on_hold"
	ProgressDone       = "done"

	SortProgress = "progress"
)

// Size values, a coarse ordinal estimate of how much work a task is, app-only
// and never synced to GitLab (GitLab CE issues have no size field — weight is
// EE), per the 000025 migration. It exists to weight velocity
// (internal/velocity) so throughput measures work finished rather than merely
// tasks finished, which splitting tasks smaller would otherwise inflate for
// free. Deliberately a five-value T-shirt scale rather than a points field:
// issue #195 rejected story points as data a human has to re-enter every task
// or it rots, and the numeric weights velocity multiplies by live in
// internal/velocity, not here. SizeM is the default, the exact middle of the
// five. SortSize is the Sort value that orders by size rank, biggest first,
// the same direction SortPriority runs.
const (
	SizeXS = "xs"
	SizeS  = "s"
	SizeM  = "m"
	SizeL  = "l"
	SizeXL = "xl"

	SortSize = "size"
)

// Actor kind values Update accepts to attribute a task_progress_events row
// (issue #169), the same vocabulary task_comments.author_kind (and
// taskcomment.AuthorKindUser/AuthorKindAgent) already uses, minus 'gitlab':
// progress is app-only and never moves via the GitLab sync path.
const (
	ActorKindUser  = "user"
	ActorKindAgent = "agent"
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
	ID                     uuid.UUID  `json:"id"`
	ProjectID              uuid.UUID  `json:"projectId"`
	BacklogID              *uuid.UUID `json:"backlogId"`
	Title                  string     `json:"title"`
	Description            string     `json:"description"`
	Status                 string     `json:"status"`
	ClosedAt               *time.Time `json:"closedAt"`
	AssigneeGitlabUserID   *int64     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string     `json:"assigneeGitlabUsername"`
	Labels                 []string   `json:"labels"`
	DueOn                  *time.Time `json:"dueOn"`
	StartDate              *time.Time `json:"startDate"`
	Priority               string     `json:"priority"`
	Progress               string     `json:"progress"`
	Size                   string     `json:"size"`
	// DesignStartedAt/ImplementationStartedAt are explicit spec-driven-development
	// phase markers, set via POST .../design-started and .../implementation-started
	// rather than derived from progress transitions — see internal/flowmetrics'
	// Design/Implementation stages.
	DesignStartedAt         *time.Time  `json:"designStartedAt"`
	ImplementationStartedAt *time.Time  `json:"implementationStartedAt"`
	Position                int32       `json:"position"`
	CreatedByUserID         uuid.UUID   `json:"createdByUserId"`
	CreatedAt               time.Time   `json:"createdAt"`
	UpdatedAt               time.Time   `json:"updatedAt"`
	Gitlab                  *GitlabInfo `json:"gitlab"`
	AIContext               AIContext   `json:"aiContext"`
}

// fromRow maps a database row to the domain model.
func fromRow(row db.Task) Task {
	return Task{
		ID:                      row.ID,
		ProjectID:               row.ProjectID,
		BacklogID:               uuidPtr(row.BacklogID),
		Title:                   row.Title,
		Description:             row.Description,
		Status:                  row.Status,
		ClosedAt:                timePtr(row.ClosedAt),
		AssigneeGitlabUserID:    int64Ptr(row.AssigneeGitlabUserID),
		AssigneeGitlabUsername:  row.AssigneeGitlabUsername,
		Labels:                  row.Labels,
		DueOn:                   datePtr(row.DueOn),
		StartDate:               datePtr(row.StartDate),
		Priority:                row.Priority,
		Progress:                row.Progress,
		Size:                    row.Size,
		DesignStartedAt:         timePtr(row.DesignStartedAt),
		ImplementationStartedAt: timePtr(row.ImplementationStartedAt),
		Position:                row.Position,
		CreatedByUserID:         row.CreatedByUserID,
		CreatedAt:               row.CreatedAt.Time,
		UpdatedAt:               row.UpdatedAt.Time,
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
	// Progress defaults to ProgressNotStarted when empty.
	Progress string
	// Size defaults to SizeM when empty.
	Size string
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
	// Progress follows the same absent/explicit-empty rule as Priority,
	// resetting to ProgressNotStarted when explicitly empty.
	Progress Optional[string]
	// Size follows the same absent/explicit-empty rule as Priority,
	// resetting to SizeM when explicitly empty.
	Size     Optional[string]
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
	Progress               string
	Size                   string
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
		Progress:               p.Progress.Or(current.Progress),
		Size:                   p.Size.Or(current.Size),
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
		return fmt.Errorf("task: authorize: %w", err)
	}
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

// normalizeSize defaults an empty raw to SizeM — the exact middle of the
// five — following normalizePriority's absent/explicit-empty rule, and
// otherwise rejects anything outside the fixed set.
func normalizeSize(raw string) (string, error) {
	switch raw {
	case "":
		return SizeM, nil
	case SizeXS, SizeS, SizeM, SizeL, SizeXL:
		return raw, nil
	default:
		return "", ErrInvalidSize
	}
}

// normalizeProgress defaults an empty raw to ProgressNotStarted, following
// normalizePriority's absent/explicit-empty rule, and otherwise rejects
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
// If the task has anywhere to be pushed — its backlog's own linked GitLab
// project, or failing that projectID's default link, see
// resolveLinkedGitlabProject — an issue.create sync job is enqueued in the
// same transaction as the task write, so the task is always eventually
// pushed (docs/plans/issue-sync.md, "Outbound"); a project with no linked
// GitLab project yet creates a purely local task. An unspecified assignee
// defaults to the project's GitLab connection's own token identity — the MVP
// has one token per project, not per user (ADR-0008), so that token's account
// stands in for "the creator's GitLab account".
func (s *Service) Create(ctx context.Context, ownerID, projectID uuid.UUID, params CreateParams) (Task, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return Task{}, err
	}

	title, err := normalizeTitle(params.Title)
	if err != nil {
		return Task{}, err
	}
	priority, err := normalizePriority(params.Priority)
	if err != nil {
		return Task{}, err
	}
	progress, err := normalizeProgress(params.Progress)
	if err != nil {
		return Task{}, err
	}
	size, err := normalizeSize(params.Size)
	if err != nil {
		return Task{}, err
	}
	if err := s.validateBacklog(ctx, ownerID, projectID, params.BacklogID); err != nil {
		return Task{}, err
	}

	var result Task
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		var err error
		result, err = createTaskInTx(ctx, q, ownerID, projectID, title, priority, progress, size, params)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return result, nil
}

// createTaskInTx does the actual insert-plus-outbox-enqueue work shared by
// Create and BulkCreate: both call it once per task from inside their own
// RunInTx closure, so a bulk request's whole batch commits or rolls back
// together. Callers are responsible for normalizing title/priority/
// progress/size and validating the backlog beforehand — this only writes.
func createTaskInTx(ctx context.Context, q db.Querier, ownerID, projectID uuid.UUID, title, priority, progress, size string, params CreateParams) (Task, error) {
	assigneeID := params.AssigneeGitlabUserID
	assigneeUsername := params.AssigneeGitlabUsername

	link, hasLink, err := resolveLinkedGitlabProject(ctx, q, ownerID, projectID, params.BacklogID)
	if err != nil {
		return Task{}, fmt.Errorf("task: create: %w", err)
	}
	if hasLink && assigneeID == nil {
		assigneeID, assigneeUsername, err = defaultAssignee(ctx, q, ownerID, projectID)
		if err != nil {
			return Task{}, fmt.Errorf("task: create: %w", err)
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
		Progress:               progress,
		Size:                   size,
		CreatedByUserID:        ownerID,
	})
	if err != nil {
		return Task{}, fmt.Errorf("task: create: %w", err)
	}
	result := fromRow(row)

	if !hasLink {
		return result, nil
	}
	payload, err := json.Marshal(issuesync.CreatePayload{
		LinkedGitlabProjectID: link.ID,
		IssuePayload:          BuildGitlabIssuePayload(result),
	})
	if err != nil {
		return Task{}, fmt.Errorf("task: create: encode sync payload: %w", err)
	}
	taskID := result.ID
	if _, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: projectID,
		TaskID:    &taskID,
		Kind:      issuesync.KindIssueCreate,
		Payload:   payload,
	}); err != nil {
		return Task{}, fmt.Errorf("task: create: enqueue sync job: %w", err)
	}
	return result, nil
}

// resolveLinkedGitlabProject decides where a new task's issue is created:
// the task's backlog's own linked GitLab project first (migration 000021),
// then projectID's default link, then nowhere at all — a project with no
// linked GitLab project yet is not an error, the task is simply created as a
// purely local one.
//
// This runs at task-create time only. Once an issue exists, its linked
// project is recorded in task_gitlab_links and every later update follows
// that row, so moving a task between backlogs afterwards never moves (or
// re-targets) the issue.
func resolveLinkedGitlabProject(ctx context.Context, q db.Querier, ownerID, projectID uuid.UUID, backlogID *uuid.UUID) (db.LinkedGitlabProject, bool, error) {
	if backlogID != nil {
		link, err := q.GetBacklogLinkedGitlabProjectForOwner(ctx, db.GetBacklogLinkedGitlabProjectForOwnerParams{
			ID:          *backlogID,
			OwnerUserID: ownerID,
		})
		if err == nil {
			return link, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return db.LinkedGitlabProject{}, false, err
		}
		// The backlog names no link of its own (or it was unlinked since):
		// fall back to the project default below.
	}
	return getDefaultLinkedGitlabProject(ctx, q, ownerID, projectID)
}

// getDefaultLinkedGitlabProject looks up projectID's default linked GitLab
// project, if any, scoped to ownerID like every other task query. A project
// with no linked GitLab project yet is not an error: the task is created as
// a purely local one.
func getDefaultLinkedGitlabProject(ctx context.Context, q db.Querier, ownerID, projectID uuid.UUID) (db.LinkedGitlabProject, bool, error) {
	link, err := q.GetDefaultLinkedGitlabProjectForOwner(ctx, db.GetDefaultLinkedGitlabProjectForOwnerParams{
		ProjectID:   projectID,
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
	Progress   string     // one of the Progress* constants, or "" (no filter)
	Size       string     // one of the Size* constants, or "" (no filter)
	// Sort is "" (position ASC, created_at ASC, the manual/DnD order),
	// SortPriority (priority rank DESC), SortProgress (progress rank ASC,
	// not_started first), SortSize (size rank DESC, biggest first),
	// SortDueOn (due date ASC, tasks with no due date last) or
	// SortUpdatedAt (most recently updated first) — the same set the
	// cross-project collection accepts, so the two lists don't disagree on
	// what a sort value means. Each keeps the manual order as its tiebreak.
	Sort string
	// AssigneeMe, when true, only returns tasks assigned to the caller's own
	// GitLab identity for this project's GitLab connection (issue #102). A
	// caller with no registered identity (internal/gitlabidentity), or a
	// project with no GitLab connection, matches nothing rather than erroring.
	AssigneeMe bool
	// Query, when non-empty, only returns tasks whose title or description
	// matches (issue #106) — see List's doc comment for how.
	Query string
}

// List returns projectID's tasks matching filter, ordered by position. It
// returns ErrNotFound if projectID does not exist or belongs to another
// user.
//
// filter.Query matches against title/description via search_vector, the
// 'simple'-config tsvector generated column (the 000016 migration,
// docs/decisions and the query's own doc comment in
// internal/database/queries/tasks.sql): no stemming, so it works for
// Japanese as long as the query matches a whole tokenized run, same as any
// other text.
func (s *Service) List(ctx context.Context, ownerID, projectID uuid.UUID, filter ListFilter) ([]Task, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.q.ListTasksByProject(ctx, db.ListTasksByProjectParams{
		OwnerUserID:    ownerID,
		ProjectID:      projectID,
		Unassigned:     filter.Unassigned,
		BacklogID:      toUUID(filter.BacklogID),
		Status:         filter.Status,
		Priority:       filter.Priority,
		Progress:       filter.Progress,
		Size:           filter.Size,
		AssigneeMe:     filter.AssigneeMe,
		Q:              filter.Query,
		SortByPriority: filter.Sort == SortPriority,
		SortByProgress: filter.Sort == SortProgress,
		SortBySize:     filter.Sort == SortSize,
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
	sortTasks(out, filter.Sort)
	return out, nil
}

// sortTasks applies the date-based orders ListFilter.Sort allows on top of
// the order the query already returned. Priority ranking stays in SQL (see
// ListTasksByProject); these two are ordered here instead because the
// project-scoped list is fetched whole — there is no LIMIT for a different
// ORDER BY to change the *contents* of, only the sequence — and adding them
// to the query would mean reshaping its parameters. A stable sort is what
// keeps the manual position order as the tiebreak in both cases.
func sortTasks(tasks []Task, by string) {
	switch by {
	case SortDueOn:
		// A task with no due date sorts last, matching the cross-project
		// list's due_on ASC NULLS LAST.
		slices.SortStableFunc(tasks, func(a, b Task) int {
			switch {
			case a.DueOn == nil && b.DueOn == nil:
				return 0
			case a.DueOn == nil:
				return 1
			case b.DueOn == nil:
				return -1
			default:
				return a.DueOn.Compare(*b.DueOn)
			}
		})
	case SortUpdatedAt:
		slices.SortStableFunc(tasks, func(a, b Task) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	}
}

// CrossProjectFilter narrows ListForOwner to a subset of every task the
// caller owns across every project. Every field's zero value means "no
// filter", the same convention ListFilter uses, except Sort and Limit, which
// ListForOwner defaults rather than leaves off (there is no manual/DnD order
// to fall back to, and an unbounded cross-project scan has no natural cap).
type CrossProjectFilter struct {
	Status        string      // "open" | "closed" | "" (no filter)
	Priority      string      // one of the Priority* constants, or "" (no filter)
	Progress      string      // one of the Progress* constants, or "" (no filter)
	Size          string      // one of the Size* constants, or "" (no filter)
	DueBefore     *time.Time  // non-nil: only tasks due on or before this date
	DueAfter      *time.Time  // non-nil: only tasks due on or after this date
	StartedBefore *time.Time  // non-nil: only tasks whose start date has arrived (start_date <= this date)
	ProjectIDs    []uuid.UUID // non-empty: only these projects, still scoped to the caller's own
	// Sort is one of SortDueOn (default), SortPriority, SortProgress,
	// SortSize or SortUpdatedAt.
	Sort string
	// Limit caps the number of tasks returned; non-positive defaults to
	// DefaultCrossProjectLimit, and anything above MaxCrossProjectLimit is
	// capped to it.
	Limit int
	// AssigneeMe, when true, only returns tasks assigned to the caller's own
	// GitLab identity for that task's own project's GitLab connection (issue
	// #102), the same per-project matching ListFilter.AssigneeMe uses.
	AssigneeMe bool
	// Query, when non-empty, only returns tasks whose title or description
	// matches, the same search_vector match ListFilter.Query uses (issue #106).
	Query string
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

	rows, err := s.q.ListTasksForMember(ctx, db.ListTasksForMemberParams{
		OwnerUserID:   ownerID,
		Status:        filter.Status,
		Priority:      filter.Priority,
		Size:          filter.Size,
		Progress:      filter.Progress,
		DueBefore:     toDate(filter.DueBefore),
		DueAfter:      toDate(filter.DueAfter),
		StartedBefore: toDate(filter.StartedBefore),
		ProjectIds:    projectIDs,
		AssigneeMe:    filter.AssigneeMe,
		Q:             filter.Query,
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

// fromCrossProjectRow maps a ListTasksForMember row to TaskWithProject. It is
// separate from fromRow because db.ListTasksForMemberRow is its own sqlc type
// (the joined project_name column keeps it from matching db.Task), not
// because the two ever disagree about a shared field.
func fromCrossProjectRow(row db.ListTasksForMemberRow) TaskWithProject {
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
			Progress:               row.Progress,
			Size:                   row.Size,
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
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
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
//
// actorKind (ActorKindUser for a session caller, ActorKindAgent for a
// bearer-token caller) attributes a task_progress_events row (issue #169),
// written in the same transaction only when progress actually changes —
// this is the sole place that table is ever written. actorUserID is
// recorded only for ActorKindUser; an agent's change has no user to
// attribute beyond that.
func (s *Service) Update(ctx context.Context, ownerID, taskID uuid.UUID, params UpdateParams, actorKind string) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
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
	progress, err := normalizeProgress(resolved.Progress)
	if err != nil {
		return Task{}, err
	}
	resolved.Progress = progress
	size, err := normalizeSize(resolved.Size)
	if err != nil {
		return Task{}, err
	}
	resolved.Size = size
	if err := s.validateBacklog(ctx, ownerID, current.ProjectID, resolved.BacklogID); err != nil {
		return Task{}, err
	}

	mirroredChanged := mirroredFieldsChanged(current, resolved)
	progressChanged := current.Progress != resolved.Progress

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
			Progress:               resolved.Progress,
			Size:                   resolved.Size,
			Position:               resolved.Position,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("task: update: %w", err)
		}
		result = fromRow(row)

		if progressChanged {
			actorUserID := &ownerID
			if actorKind == ActorKindAgent {
				actorUserID = nil
			}
			if _, err := q.CreateTaskProgressEvent(ctx, db.CreateTaskProgressEventParams{
				TaskID:       taskID,
				FromProgress: current.Progress,
				ToProgress:   resolved.Progress,
				ActorKind:    actorKind,
				ActorUserID:  toUUID(actorUserID),
			}); err != nil {
				return fmt.Errorf("task: update: record progress event: %w", err)
			}
		}

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
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
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

// Reorder resequences the position of every task in one backlog bucket
// (backlogID's tasks, or the Unclassified group when backlogID is nil) to
// match taskIDs' order — position 0 for the first ID, 1 for the second, and
// so on. taskIDs must be exactly that bucket's current task set (same
// length, no duplicates, nothing missing or foreign), or it returns
// ErrTaskIDsMismatch without writing anything: a partial reorder would leave
// some tasks with a stale position relative to their new neighbors, and the
// D&D UI always knows the bucket's full membership before it calls this
// (issue #79). Moving a task to a *different* backlog is a separate step
// (AssignBacklog or Update's BacklogID), done before calling Reorder for the
// destination bucket — this method never changes backlog_id itself.
func (s *Service) Reorder(ctx context.Context, ownerID, projectID uuid.UUID, backlogID *uuid.UUID, taskIDs []uuid.UUID) ([]Task, error) {
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return nil, err
	}
	if err := s.validateBacklog(ctx, ownerID, projectID, backlogID); err != nil {
		return nil, err
	}

	filter := ListFilter{}
	if backlogID != nil {
		filter.BacklogID = backlogID
	} else {
		filter.Unassigned = true
	}
	current, err := s.q.ListTasksByProject(ctx, db.ListTasksByProjectParams{
		ProjectID:  projectID,
		Unassigned: filter.Unassigned,
		BacklogID:  toUUID(filter.BacklogID),
	})
	if err != nil {
		return nil, fmt.Errorf("task: reorder: %w", err)
	}
	if !sameTaskIDSet(current, taskIDs) {
		return nil, ErrTaskIDsMismatch
	}

	if err := s.q.ReorderTasks(ctx, db.ReorderTasksParams{
		TaskIds:   taskIDs,
		ProjectID: projectID,
		BacklogID: toUUID(backlogID),
	}); err != nil {
		return nil, fmt.Errorf("task: reorder: %w", err)
	}

	return s.List(ctx, ownerID, projectID, filter)
}

// sameTaskIDSet reports whether taskIDs is exactly current's task IDs, in
// any order: same length, no duplicates, nothing missing or foreign.
func sameTaskIDSet(current []db.Task, taskIDs []uuid.UUID) bool {
	if len(current) != len(taskIDs) {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		if _, dup := seen[id]; dup {
			return false
		}
		seen[id] = struct{}{}
	}
	for _, t := range current {
		if _, ok := seen[t.ID]; !ok {
			return false
		}
	}
	return true
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
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
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
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
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

// MarkDesignStarted records the current time as the task's design phase
// start (spec-driven development): unlike most task fields, this always
// overwrites, so calling it again (e.g. redoing the design after a review
// comment) just moves the timestamp forward. App-only, never synced to
// GitLab.
func (s *Service) MarkDesignStarted(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return Task{}, err
	}
	row, err := s.q.MarkTaskDesignStarted(ctx, db.MarkTaskDesignStartedParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: mark design started: %w", err)
	}
	return fromRow(row), nil
}

// MarkImplementationStarted is MarkDesignStarted's implementation-phase
// counterpart. The two are independent: a task with an implementation
// start but no design start simply skipped the design phase.
func (s *Service) MarkImplementationStarted(ctx context.Context, ownerID, taskID uuid.UUID) (Task, error) {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return Task{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return Task{}, err
	}
	row, err := s.q.MarkTaskImplementationStarted(ctx, db.MarkTaskImplementationStartedParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("task: mark implementation started: %w", err)
	}
	return fromRow(row), nil
}

// Delete removes the task and never touches GitLab, even for a linked task:
// the ON DELETE CASCADE on task_gitlab_links/sync_jobs simply forgets the
// link, so a later re-sync does not resurrect it, per
// docs/plans/issue-sync.md. Ownership is enforced by the query, so a
// non-owner gets ErrNotFound and nothing is deleted.
func (s *Service) Delete(ctx context.Context, ownerID, taskID uuid.UUID) error {
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
		return err
	}
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
	current, err := s.Get(ctx, ownerID, taskID)
	if err != nil {
		return AIContext{}, err
	}
	if err := s.authorize(ctx, ownerID, current.ProjectID, project.RoleMember); err != nil {
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

// ProgressGuidance is fixed instruction text embedded in every AI-facing
// Context response (issue #170). The context API is the only thing a VS
// Code agent working through a project API token reliably reads on every
// task — human-only documentation like this README never reaches it — so
// the progress transition convention has to travel inside the response
// itself for lead-time measurement (issue #169's task_progress_events) to
// have any events to measure at all.
const ProgressGuidance = `Update this task's progress as you work, via PATCH /api/v1/tasks/{taskID} with {"progress": "..."}: set "in_progress" when you start work, "on_hold" when you are blocked or waiting on something outside this task (this is what separates wait time from work time in FlowLens's metrics — use it whenever you are stalled), and "done" when the work is finished. Never set "status" yourself — that mirrors the GitLab issue's open/closed state and is kept in sync automatically.`

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
	ID               uuid.UUID  `json:"id"`
	ProjectID        uuid.UUID  `json:"projectId"`
	BacklogID        *uuid.UUID `json:"backlogId"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Status           string     `json:"status"`
	Progress         string     `json:"progress"`
	ProgressGuidance string     `json:"progressGuidance"`
	// Size is the task's coarse size estimate (one of the Size* constants),
	// surfaced so an agent knows how large the work is expected to be before
	// it starts. App-only, never synced to GitLab.
	Size                   string     `json:"size"`
	AssigneeGitlabUserID   *int64     `json:"assigneeGitlabUserId"`
	AssigneeGitlabUsername string     `json:"assigneeGitlabUsername"`
	Labels                 []string   `json:"labels"`
	DueOn                  *time.Time `json:"dueOn"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	// BaseBranch is the task's backlog's base_branch (internal/backlog,
	// issue's own migration) — the branch an agent working this task should
	// branch from. "" when the task is unfiled or its backlog has none set.
	BaseBranch         string         `json:"baseBranch"`
	Gitlab             *GitlabContext `json:"gitlab"`
	AcceptanceCriteria string         `json:"acceptanceCriteria"`
	AIContext          string         `json:"aiContext"`
	AllowedScope       string         `json:"allowedScope"`
	ForbiddenScope     string         `json:"forbiddenScope"`
}

// toContext combines t with its already-resolved GitLab and AI context into
// the AI-facing Context shape.
func toContext(t Task, gc *GitlabContext, ai AIContext, baseBranch string) Context {
	return Context{
		ID:                     t.ID,
		ProjectID:              t.ProjectID,
		BacklogID:              t.BacklogID,
		Title:                  t.Title,
		Description:            t.Description,
		Status:                 t.Status,
		Progress:               t.Progress,
		ProgressGuidance:       ProgressGuidance,
		Size:                   t.Size,
		AssigneeGitlabUserID:   t.AssigneeGitlabUserID,
		AssigneeGitlabUsername: t.AssigneeGitlabUsername,
		Labels:                 t.Labels,
		DueOn:                  t.DueOn,
		UpdatedAt:              t.UpdatedAt,
		BaseBranch:             baseBranch,
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
	baseBranch, err := s.baseBranchForTask(ctx, t.BacklogID)
	if err != nil {
		return Context{}, err
	}
	return toContext(t, gc, ai, baseBranch), nil
}

// baseBranchForTask resolves backlogID's base_branch, or "" if the task is
// unfiled (backlogID nil) or its backlog has none set.
func (s *Service) baseBranchForTask(ctx context.Context, backlogID *uuid.UUID) (string, error) {
	if backlogID == nil {
		return "", nil
	}
	branch, err := s.q.GetBacklogBaseBranch(ctx, *backlogID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("task: get backlog base branch: %w", err)
	}
	return branch, nil
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
	if err := s.authorize(ctx, ownerID, projectID, project.RoleViewer); err != nil {
		return ContextPage{}, err
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
