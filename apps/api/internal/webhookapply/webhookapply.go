// Package webhookapply applies pending webhook_events rows to tasks
// (docs/plans/issue-sync.md, "Inbound"): create, update, close and reopen,
// guarded against an out-of-scope issue, a stale or duplicate delivery, and
// FlowLens's own outbound push echoing back. Since #104 it also applies
// "Note Hook" events onto a task's activity log (task_comments) — see
// applyNote. It never enqueues an outbound
// sync_jobs row — apply's write path only ever touches tasks,
// task_gitlab_links and webhook_events, and never imports internal/sync —
// which is the structural half of the loop-prevention guard; the
// content-fingerprint echo check is the other half.
package webhookapply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/linkedproject"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Skip reasons stored in webhook_events.skip_reason by this package.
// internal/webhookevent separately owns SkipReasonUnsupportedEvent, applied
// by the receiver before an event ever reaches here.
const (
	SkipReasonOutOfScope = "out_of_scope"
	SkipReasonStale      = "stale"
	SkipReasonEcho       = "echo"
)

// issueWebhookPayload is the subset of a GitLab "Issue Hook" delivery this
// package needs to apply an event to a task. internal/http's receiver
// parses only enough of the same payload to record the row (see
// gitlabIssueHookPayload there); this is the fuller decode the apply
// pipeline needs.
type issueWebhookPayload struct {
	ObjectAttributes struct {
		ID          int64  `json:"id"`
		IID         int64  `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       string `json:"state"` // "opened" | "closed"
		DueDate     string `json:"due_date"`
		UpdatedAt   string `json:"updated_at"`
		URL         string `json:"url"`
	} `json:"object_attributes"`
	Labels []struct {
		Title string `json:"title"`
	} `json:"labels"`
	Assignees []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"assignees"`
}

// taskFields is a webhook payload's issue fields, normalized into the shape
// both the scope check and the tasks/task_gitlab_links writes need.
type taskFields struct {
	Title            string
	Description      string
	Status           string
	Labels           []string
	DueDate          *time.Time
	AssigneeID       *int64
	AssigneeUsername string
	UpdatedAt        time.Time
	IssueID          int64
	IssueIID         int64
	WebURL           string
	Fingerprint      string
}

func fieldsFromPayload(p issueWebhookPayload) taskFields {
	labels := make([]string, 0, len(p.Labels))
	for _, l := range p.Labels {
		labels = append(labels, l.Title)
	}

	var assigneeID *int64
	var assigneeUsername string
	var assigneeIDs []int64
	if len(p.Assignees) > 0 {
		id := p.Assignees[0].ID
		assigneeID = &id
		assigneeUsername = p.Assignees[0].Username
		assigneeIDs = []int64{id}
	}

	dueDate := parseGitlabDate(p.ObjectAttributes.DueDate)

	status := task.StatusOpen
	if p.ObjectAttributes.State == "closed" {
		status = task.StatusClosed
	}

	return taskFields{
		Title:            p.ObjectAttributes.Title,
		Description:      p.ObjectAttributes.Description,
		Status:           status,
		Labels:           labels,
		DueDate:          dueDate,
		AssigneeID:       assigneeID,
		AssigneeUsername: assigneeUsername,
		UpdatedAt:        parseGitlabTime(p.ObjectAttributes.UpdatedAt),
		IssueID:          p.ObjectAttributes.ID,
		IssueIID:         p.ObjectAttributes.IID,
		WebURL:           p.ObjectAttributes.URL,
		Fingerprint:      gitlab.Fingerprint(p.ObjectAttributes.Title, p.ObjectAttributes.Description, labels, dueDate, assigneeIDs),
	}
}

// noteWebhookPayload is the subset of a GitLab "Note Hook" delivery this
// package needs to apply a comment to a task (#104). Only notes on an issue
// (NoteableType "Issue") that aren't GitLab-generated (System false, e.g.
// "changed the description") are mirrored in.
type noteWebhookPayload struct {
	ObjectAttributes struct {
		ID           int64  `json:"id"`
		Note         string `json:"note"`
		NoteableType string `json:"noteable_type"`
		System       bool   `json:"system"`
	} `json:"object_attributes"`
	Issue struct {
		IID int64 `json:"iid"`
	} `json:"issue"`
}

// Service applies pending webhook_events rows. Like internal/issuesync's
// Service, it runs as the background worker: the linked project a payload
// names was already authorized when its webhook was registered, so its
// queries are unscoped.
type Service struct {
	txRunner database.TxRunner
}

// NewService constructs a webhookapply Service. txRunner scopes every
// claim-and-apply to one atomic unit — see ProcessNext's doc comment.
func NewService(txRunner database.TxRunner) *Service {
	return &Service{txRunner: txRunner}
}

// ProcessNext claims the oldest pending webhook_events row and applies it,
// all within one transaction: the FOR UPDATE SKIP LOCKED claim and the
// resulting task write and event status update commit together, so a crash
// mid-apply leaves the event 'pending' for the next poll to retry rather
// than stuck half-applied. It reports whether an event was claimed, so a
// Worker can decide whether to keep draining or wait for the next tick.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	claimed := false
	err := s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		event, err := q.ClaimNextPendingWebhookEvent(ctx)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("webhookapply: claim event: %w", err)
		}
		claimed = true
		return s.apply(ctx, q, event)
	})
	return claimed, err
}

// apply decides one of five outcomes for event and records it: an
// out-of-scope, stale or echo skip, a task create/update, or a failure. An
// error return aborts and rolls back the whole claim-and-apply transaction
// (see ProcessNext), leaving the event 'pending' for a retry; every expected
// outcome is instead recorded through the mark* helpers, which always
// return nil so the transaction commits.
func (s *Service) apply(ctx context.Context, q db.Querier, event db.WebhookEvent) error {
	if event.ObjectKind == "note" {
		return s.applyNote(ctx, q, event)
	}

	var payload issueWebhookPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("decode payload: %w", err))
	}

	link, err := q.GetLinkedGitlabProjectByID(ctx, event.LinkedGitlabProjectID)
	if err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("get linked project: %w", err))
	}

	fields := fieldsFromPayload(payload)

	if !linkedproject.InScope(fields.Labels, link.SyncScope, link.SyncLabels) {
		return s.markSkipped(ctx, q, event.ID, SkipReasonOutOfScope)
	}

	existingLink, err := q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
		LinkedGitlabProjectID: event.LinkedGitlabProjectID,
		GitlabIssueIid:        fields.IssueIID,
	})
	switch {
	case err == nil:
		return s.applyToExistingTask(ctx, q, event, link, existingLink, fields)
	case errors.Is(err, pgx.ErrNoRows):
		return s.applyAsNewTask(ctx, q, event, link, fields)
	default:
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("get task link: %w", err))
	}
}

// applyNote is the comment-sync counterpart of apply (#104): unlike an issue
// event, a comment has no single-column baseline to stale/echo-check against
// (task_gitlab_links.last_pushed_fingerprint is per-task, not per-comment),
// so its loop prevention is entirely per-row instead — a note whose GitLab id
// already exists on some task_comments row is FlowLens's own push echoing
// back (see internal/commentsync.HandleCommentCreate, which stores the id
// there right after a successful push).
func (s *Service) applyNote(ctx context.Context, q db.Querier, event db.WebhookEvent) error {
	var payload noteWebhookPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("decode note payload: %w", err))
	}

	if payload.ObjectAttributes.NoteableType != "Issue" || payload.ObjectAttributes.System {
		return s.markSkipped(ctx, q, event.ID, SkipReasonOutOfScope)
	}

	link, err := q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
		LinkedGitlabProjectID: event.LinkedGitlabProjectID,
		GitlabIssueIid:        payload.Issue.IID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The issue itself isn't tracked as a task (out of sync scope, or
			// its own create event hasn't been applied yet) — nothing to
			// attach the comment to.
			return s.markSkipped(ctx, q, event.ID, SkipReasonOutOfScope)
		}
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("get task link: %w", err))
	}

	noteID := pgtype.Int8{Int64: payload.ObjectAttributes.ID, Valid: true}
	if _, err := q.GetTaskCommentByGitlabNoteID(ctx, noteID); err == nil {
		return s.markSkipped(ctx, q, event.ID, SkipReasonEcho)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("check existing comment: %w", err))
	}

	if _, err := q.CreateGitlabTaskComment(ctx, db.CreateGitlabTaskCommentParams{
		TaskID:       link.TaskID,
		Body:         payload.ObjectAttributes.Note,
		GitlabNoteID: noteID,
	}); err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("create comment: %w", err))
	}

	return s.markProcessed(ctx, q, event.ID)
}

// applyToExistingTask is the "known issue" path: guard 2 (stale) and guard 3
// (echo) run here, since both need a baseline — task_gitlab_links.
// gitlab_updated_at and last_pushed_fingerprint — that only a known issue
// has.
func (s *Service) applyToExistingTask(ctx context.Context, q db.Querier, event db.WebhookEvent, linkedProject db.LinkedGitlabProject, link db.TaskGitlabLink, fields taskFields) error {
	// Strictly Before, not "not After": a delivery whose updated_at ties the
	// recorded baseline is a legitimate near-simultaneous update (e.g. the
	// loser of the guard-1 create race retrying into this path), not a
	// reordered redelivery. If it also carries identical content that's the
	// echo guard's job, right below.
	if !fields.UpdatedAt.IsZero() && link.GitlabUpdatedAt.Valid && fields.UpdatedAt.Before(link.GitlabUpdatedAt.Time) {
		return s.markSkipped(ctx, q, event.ID, SkipReasonStale)
	}
	if fields.Fingerprint == link.LastPushedFingerprint {
		// last_pushed_fingerprint never changes on a close/reopen push
		// (handleStateChange in internal/issuesync intentionally leaves it
		// alone — nothing content-wise changed), so a genuine external
		// close/reopen whose title/description/labels/due date/assignee
		// happen to still match the last content push would otherwise be
		// indistinguishable from FlowLens's own echo. Only treat this as an
		// echo if the task's current status also already matches: FlowLens's
		// own edits (content or state) always land on the task row before the
		// outbound push is even enqueued, so by the time its echo arrives the
		// task is already in that state; an external close/reopen is not.
		unchanged, err := s.statusAlreadyApplied(ctx, q, linkedProject, link.TaskID, fields.Status)
		if err != nil {
			return s.markFailed(ctx, q, event.ID, fmt.Errorf("check current status: %w", err))
		}
		if unchanged {
			return s.markSkipped(ctx, q, event.ID, SkipReasonEcho)
		}
	}

	if _, err := q.ApplyWebhookTaskFields(ctx, db.ApplyWebhookTaskFieldsParams{
		ID:                     link.TaskID,
		Title:                  fields.Title,
		Description:            fields.Description,
		AssigneeGitlabUserID:   toInt8(fields.AssigneeID),
		AssigneeGitlabUsername: fields.AssigneeUsername,
		Labels:                 fields.Labels,
		DueOn:                  toDate(fields.DueDate),
		Status:                 fields.Status,
	}); err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("update task: %w", err))
	}

	if _, err := q.MarkTaskGitlabLinkAppliedForTask(ctx, db.MarkTaskGitlabLinkAppliedForTaskParams{
		TaskID:          link.TaskID,
		GitlabUpdatedAt: toTimestamptz(fields.UpdatedAt),
	}); err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("mark link applied: %w", err))
	}

	return s.markProcessed(ctx, q, event.ID)
}

// applyAsNewTask is the "unknown issue" path: no existing task_gitlab_links
// row matches this issue, so a new unclassified task (backlog_id = NULL) is
// created and linked to it.
func (s *Service) applyAsNewTask(ctx context.Context, q db.Querier, event db.WebhookEvent, link db.LinkedGitlabProject, fields taskFields) error {
	ownerID, projectID, err := resolveProjectOwner(ctx, q, link)
	if err != nil {
		return s.markFailed(ctx, q, event.ID, err)
	}

	taskRow, err := q.CreateTask(ctx, db.CreateTaskParams{
		ProjectID:              projectID,
		Title:                  fields.Title,
		Description:            fields.Description,
		AssigneeGitlabUserID:   toInt8(fields.AssigneeID),
		AssigneeGitlabUsername: fields.AssigneeUsername,
		Labels:                 fields.Labels,
		DueOn:                  toDate(fields.DueDate),
		Priority:               task.PriorityMedium,
		Progress:               task.ProgressNotStarted,
		CreatedByUserID:        ownerID,
	})
	if err != nil {
		return s.markFailed(ctx, q, event.ID, fmt.Errorf("create task: %w", err))
	}

	if fields.Status == task.StatusClosed {
		if _, err := q.ApplyWebhookTaskFields(ctx, db.ApplyWebhookTaskFieldsParams{
			ID:                     taskRow.ID,
			Title:                  fields.Title,
			Description:            fields.Description,
			AssigneeGitlabUserID:   toInt8(fields.AssigneeID),
			AssigneeGitlabUsername: fields.AssigneeUsername,
			Labels:                 fields.Labels,
			DueOn:                  toDate(fields.DueDate),
			Status:                 task.StatusClosed,
		}); err != nil {
			return s.markFailed(ctx, q, event.ID, fmt.Errorf("close new task: %w", err))
		}
	}

	if _, err := q.CreateTaskGitlabLink(ctx, db.CreateTaskGitlabLinkParams{
		TaskID:                taskRow.ID,
		LinkedGitlabProjectID: event.LinkedGitlabProjectID,
		GitlabIssueID:         fields.IssueID,
		GitlabIssueIid:        fields.IssueIID,
		GitlabWebUrl:          fields.WebURL,
		GitlabUpdatedAt:       toTimestamptz(fields.UpdatedAt),
		LastPushedFingerprint: fields.Fingerprint,
	}); err != nil {
		// Guard 1 (duplicate): a concurrent event for the same new issue may
		// have already created and linked a task — the UNIQUE
		// (linked_gitlab_project_id, gitlab_issue_iid) constraint fires here.
		// Returning the raw error rolls back this whole transaction,
		// including the task just created above, and leaves the event
		// 'pending'; the next poll re-applies it and this time finds the
		// link already there, taking the update path instead. So no
		// mark-failed here — this is not a failure, it is a retry signal.
		return fmt.Errorf("webhookapply: create task link: %w", err)
	}

	return s.markProcessed(ctx, q, event.ID)
}

// resolveProjectOwner walks a linked GitLab project back to its app
// project's owner, for CreateTask's created_by_user_id — a webhook apply has
// no acting user of its own, so the project owner stands in, the same way
// internal/task.Service.Create's default-assignee resolution stands in for
// "the creator's GitLab account" (docs/plans/issue-sync.md).
func resolveProjectOwner(ctx context.Context, q db.Querier, link db.LinkedGitlabProject) (ownerID, projectID uuid.UUID, err error) {
	conn, err := q.GetGitlabConnectionByID(ctx, link.GitlabConnectionID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("get gitlab connection: %w", err)
	}
	proj, err := q.GetProjectByID(ctx, conn.ProjectID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("get project: %w", err)
	}
	return proj.OwnerUserID, proj.ID, nil
}

// statusAlreadyApplied reports whether taskID's current status already
// equals status, the extra signal the echo guard (applyToExistingTask) needs
// once a content fingerprint match alone can no longer tell a genuine
// external close/reopen from FlowLens's own echo.
func (s *Service) statusAlreadyApplied(ctx context.Context, q db.Querier, linkedProject db.LinkedGitlabProject, taskID uuid.UUID, status string) (bool, error) {
	ownerID, _, err := resolveProjectOwner(ctx, q, linkedProject)
	if err != nil {
		return false, err
	}
	current, err := q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: taskID, OwnerUserID: ownerID})
	if err != nil {
		return false, fmt.Errorf("get task: %w", err)
	}
	return current.Status == status, nil
}

func (s *Service) markProcessed(ctx context.Context, q db.Querier, id uuid.UUID) error {
	if err := q.MarkWebhookEventProcessed(ctx, id); err != nil {
		return fmt.Errorf("webhookapply: mark processed: %w", err)
	}
	return nil
}

func (s *Service) markSkipped(ctx context.Context, q db.Querier, id uuid.UUID, reason string) error {
	if err := q.MarkWebhookEventSkipped(ctx, db.MarkWebhookEventSkippedParams{ID: id, SkipReason: reason}); err != nil {
		return fmt.Errorf("webhookapply: mark skipped: %w", err)
	}
	return nil
}

// markFailed records why apply could not be completed on event itself
// (webhook_events.error_message) and returns nil so the transaction still
// commits — a decode error or a missing linked project is recorded as
// 'failed', not retried forever the way an unexpected DB error is (see
// apply's doc comment).
func (s *Service) markFailed(ctx context.Context, q db.Querier, id uuid.UUID, cause error) error {
	if err := q.MarkWebhookEventFailed(ctx, db.MarkWebhookEventFailedParams{ID: id, ErrorMessage: cause.Error()}); err != nil {
		return fmt.Errorf("webhookapply: mark failed: %w (after: %v)", err, cause)
	}
	return nil
}

func toInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func toDate(v *time.Time) pgtype.Date {
	if v == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *v, Valid: true}
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// parseGitlabTime parses a GitLab webhook payload timestamp
// (object_attributes.updated_at, RFC3339). An empty or unparsable value
// returns the zero time.
func parseGitlabTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseGitlabDate parses a GitLab webhook payload date-only field
// (object_attributes.due_date, "YYYY-MM-DD"). An empty or unparsable value
// returns nil.
func parseGitlabDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// Default tuning for a Worker.
const DefaultPollInterval = 5 * time.Second

// Worker polls ProcessNext in a loop: draining every pending event
// immediately, then sleeping PollInterval once a poll finds nothing to
// claim. It mirrors internal/sync.Worker's Run/Stop shape, adapted to
// ProcessNext's own claim-and-apply-in-one-call design (webhook_events has
// no 'running'/attempts/run_after machinery to poll around, unlike
// sync_jobs).
type Worker struct {
	service      *Service
	pollInterval time.Duration

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

// Option configures a Worker at construction time.
type Option func(*Worker)

// WithPollInterval sets how long Run sleeps between polls when the previous
// poll claimed no event. Default: DefaultPollInterval.
func WithPollInterval(d time.Duration) Option { return func(w *Worker) { w.pollInterval = d } }

// NewWorker constructs a Worker over service.
func NewWorker(service *Service, opts ...Option) *Worker {
	w := &Worker{
		service:      service,
		pollInterval: DefaultPollInterval,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Run polls ProcessNext — looping immediately after a poll that claimed an
// event, and sleeping PollInterval after one that claimed nothing — until
// Stop is called or ctx is done. ctx also bounds every claim/apply call Run
// makes, so it should normally be a long-lived context (e.g.
// context.Background()), not one tied to shutdown: cancelling it aborts
// whatever event is currently being applied rather than letting it finish.
// Stop is the graceful-shutdown signal.
func (w *Worker) Run(ctx context.Context) error {
	defer close(w.done)

	for {
		select {
		case <-w.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		claimed, err := w.service.ProcessNext(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "webhook apply worker: process failed", "error", err)
		}
		if claimed {
			continue
		}

		select {
		case <-w.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.pollInterval):
		}
	}
}

// Stop tells Run to stop claiming new events, then blocks until Run returns
// (i.e. until any event it is currently applying finishes) or ctx is done,
// whichever comes first. It is safe to call more than once.
func (w *Worker) Stop(ctx context.Context) error {
	w.stopOnce.Do(func() { close(w.stop) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
