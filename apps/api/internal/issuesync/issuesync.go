// Package issuesync implements the outbound sync_jobs handlers that push
// task creates, edits, closes and reopens to GitLab CE issues
// (docs/plans/issue-sync.md, "Outbound"). Handlers are registered into an
// internal/sync.Registry by kind at wiring time; internal/task builds and
// enqueues the jobs this package executes, so the two packages share the
// payload types defined here.
package issuesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Job kinds this package handles, registered into an internal/sync.Registry
// by internal/http wiring (cmd/api/main.go).
const (
	KindIssueCreate = "issue.create"
	KindIssueUpdate = "issue.update"
	KindIssueClose  = "issue.close"
	KindIssueReopen = "issue.reopen"
)

// CreatePayload is the sync_jobs.payload shape for KindIssueCreate, built by
// internal/task.Service.Create.
type CreatePayload struct {
	LinkedGitlabProjectID uuid.UUID `json:"linkedGitlabProjectId"`
	gitlab.IssuePayload
}

// UpdatePayload is the sync_jobs.payload shape for KindIssueUpdate, built by
// internal/task.Service.Update. KindIssueClose/KindIssueReopen carry no
// payload: the task's linked GitLab project and issue IID come from
// task_gitlab_links, and the only change being pushed is state_event.
type UpdatePayload struct {
	gitlab.UpdateIssuePayload
}

// Service executes outbound issue sync jobs. Unlike internal/gitlabconn,
// which scopes every lookup to an acting user's session, Service runs as
// the background worker: the job row itself is the authorization (it was
// enqueued by an owner-scoped handler already), so its queries are
// unscoped.
type Service struct {
	q             db.Querier
	cipher        *crypto.Cipher
	clientFactory gitlabconn.ClientFactory
}

// NewService constructs a Service. clientFactory builds the gitlab.Client
// used to call a linked project's GitLab instance; cipher decrypts its
// connection's stored access token.
func NewService(q db.Querier, cipher *crypto.Cipher, clientFactory gitlabconn.ClientFactory) *Service {
	return &Service{q: q, cipher: cipher, clientFactory: clientFactory}
}

func taskIDFromJob(job db.SyncJob) (uuid.UUID, error) {
	if !job.TaskID.Valid {
		return uuid.Nil, fmt.Errorf("issuesync: job %s (%s) has no task_id", job.ID, job.Kind)
	}
	return uuid.UUID(job.TaskID.Bytes), nil
}

// dial resolves the gitlab.Client, decrypted access token, and linked
// project row (which carries the GitLab-side numeric project ID) to call
// for linkedProjectID.
func (s *Service) dial(ctx context.Context, linkedProjectID uuid.UUID) (gitlab.Client, string, db.LinkedGitlabProject, error) {
	link, err := s.q.GetLinkedGitlabProjectByID(ctx, linkedProjectID)
	if err != nil {
		return nil, "", db.LinkedGitlabProject{}, fmt.Errorf("issuesync: get linked project: %w", err)
	}
	conn, err := s.q.GetGitlabConnectionByID(ctx, link.GitlabConnectionID)
	if err != nil {
		return nil, "", db.LinkedGitlabProject{}, fmt.Errorf("issuesync: get gitlab connection: %w", err)
	}
	token, err := s.cipher.Decrypt(conn.EncryptedToken)
	if err != nil {
		return nil, "", db.LinkedGitlabProject{}, fmt.Errorf("issuesync: decrypt token: %w", err)
	}
	return s.clientFactory(conn.BaseUrl), token, link, nil
}

// HandleIssueCreate creates a GitLab issue for a newly created task and
// records the link. It is idempotent both against a direct re-run — the
// existing task_gitlab_links row short-circuits it before any GitLab call —
// and against the check-then-insert race between two attempts: a UNIQUE
// violation on insert is treated as the job having already succeeded,
// rather than an error, per docs/plans/issue-sync.md's "issue.create" note.
func (s *Service) HandleIssueCreate(ctx context.Context, job db.SyncJob) error {
	taskID, err := taskIDFromJob(job)
	if err != nil {
		return err
	}

	if _, err := s.q.GetTaskGitlabLinkByTaskID(ctx, taskID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("issuesync: check existing link: %w", err)
	}

	var payload CreatePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("issuesync: decode payload: %w", err)
	}

	client, token, link, err := s.dial(ctx, payload.LinkedGitlabProjectID)
	if err != nil {
		return err
	}

	issue, err := client.CreateIssue(ctx, token, link.GitlabProjectID, payload.IssuePayload)
	if err != nil {
		return fmt.Errorf("issuesync: create issue: %w", err)
	}

	fingerprint := gitlab.Fingerprint(payload.Title, payload.Description, payload.Labels, payload.DueDate, payload.AssigneeIDs)
	_, err = s.q.CreateTaskGitlabLink(ctx, db.CreateTaskGitlabLinkParams{
		TaskID:                taskID,
		LinkedGitlabProjectID: payload.LinkedGitlabProjectID,
		GitlabIssueID:         issue.ID,
		GitlabIssueIid:        issue.IID,
		GitlabWebUrl:          issue.WebURL,
		GitlabUpdatedAt:       toTimestamptz(issue.UpdatedAt),
		LastPushedFingerprint: fingerprint,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil
		}
		return fmt.Errorf("issuesync: save link: %w", err)
	}
	return nil
}

// HandleIssueUpdate pushes a task's mirrored fields (title, description,
// assignee, labels, due date) to its linked GitLab issue. A task with no
// link — never created, or its link since removed — is a no-op success:
// there is nothing to push.
func (s *Service) HandleIssueUpdate(ctx context.Context, job db.SyncJob) error {
	taskID, err := taskIDFromJob(job)
	if err != nil {
		return err
	}

	link, err := s.q.GetTaskGitlabLinkByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("issuesync: get link: %w", err)
	}

	var payload UpdatePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("issuesync: decode payload: %w", err)
	}

	client, token, linkedProject, err := s.dial(ctx, link.LinkedGitlabProjectID)
	if err != nil {
		return s.markFailed(ctx, taskID, err)
	}

	issue, err := client.UpdateIssue(ctx, token, linkedProject.GitlabProjectID, link.GitlabIssueIid, payload.UpdateIssuePayload)
	if err != nil {
		return s.markFailed(ctx, taskID, fmt.Errorf("issuesync: update issue: %w", err))
	}

	var title, description string
	if payload.Title != nil {
		title = *payload.Title
	}
	if payload.Description != nil {
		description = *payload.Description
	}
	fingerprint := gitlab.Fingerprint(title, description, payload.Labels, payload.DueDate, payload.AssigneeIDs)
	return s.markSynced(ctx, taskID, issue, fingerprint)
}

// HandleIssueClose closes a task's linked GitLab issue. A task with no link
// is a no-op success, the same as HandleIssueUpdate.
func (s *Service) HandleIssueClose(ctx context.Context, job db.SyncJob) error {
	return s.handleStateChange(ctx, job, "close")
}

// HandleIssueReopen reopens a task's linked GitLab issue. A task with no
// link is a no-op success, the same as HandleIssueUpdate.
func (s *Service) HandleIssueReopen(ctx context.Context, job db.SyncJob) error {
	return s.handleStateChange(ctx, job, "reopen")
}

// handleStateChange sends state_event=stateEvent to the task's linked
// issue, leaving every other field untouched. It never touches
// last_pushed_fingerprint (nothing content-wise changed), keeping whichever
// value the last create/update push left.
func (s *Service) handleStateChange(ctx context.Context, job db.SyncJob, stateEvent string) error {
	taskID, err := taskIDFromJob(job)
	if err != nil {
		return err
	}

	link, err := s.q.GetTaskGitlabLinkByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("issuesync: get link: %w", err)
	}

	client, token, linkedProject, err := s.dial(ctx, link.LinkedGitlabProjectID)
	if err != nil {
		return s.markFailed(ctx, taskID, err)
	}

	issue, err := client.UpdateIssue(ctx, token, linkedProject.GitlabProjectID, link.GitlabIssueIid, gitlab.UpdateIssuePayload{StateEvent: stateEvent})
	if err != nil {
		return s.markFailed(ctx, taskID, fmt.Errorf("issuesync: %s issue: %w", stateEvent, err))
	}
	return s.markSynced(ctx, taskID, issue, link.LastPushedFingerprint)
}

// markSynced records a successful push: sync_status='synced', last_error
// cleared, gitlab_updated_at and last_synced_at refreshed.
func (s *Service) markSynced(ctx context.Context, taskID uuid.UUID, issue *gitlab.Issue, fingerprint string) error {
	_, err := s.q.MarkTaskGitlabLinkSyncedForTask(ctx, db.MarkTaskGitlabLinkSyncedForTaskParams{
		TaskID:                taskID,
		GitlabUpdatedAt:       toTimestamptz(issue.UpdatedAt),
		LastPushedFingerprint: fingerprint,
	})
	if err != nil {
		return fmt.Errorf("issuesync: mark link synced: %w", err)
	}
	return nil
}

// markFailed records why a push failed on the task's link, then returns
// cause unchanged so internal/sync's generic retry/backoff still applies —
// this only records the task_gitlab_links-side outcome, it does not decide
// whether the job itself retries.
func (s *Service) markFailed(ctx context.Context, taskID uuid.UUID, cause error) error {
	if _, err := s.q.MarkTaskGitlabLinkFailedForTask(ctx, db.MarkTaskGitlabLinkFailedForTaskParams{
		TaskID:    taskID,
		LastError: cause.Error(),
	}); err != nil {
		return fmt.Errorf("issuesync: mark link failed: %w (after: %v)", err, cause)
	}
	return cause
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation — for task_gitlab_links this can only be the task_id primary
// key (a concurrent create already linked this task) or the
// (linked_gitlab_project_id, gitlab_issue_iid) unique constraint.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
