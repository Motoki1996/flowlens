// Package projectsync implements the project.import and project.resync
// sync_jobs handlers (docs/plans/issue-sync.md's "project.import" /
// "project.resync" outbound jobs, issue #25): walking every page of a
// linked GitLab project's issues and creating or updating the app's
// unclassified tasks from them, recording each attempt as a
// gitlab_sync_runs row (start/end, counts, error). project.import is
// enqueued automatically when a GitLab project is linked (see EnqueueImport,
// called from internal/linkedproject.Service.Create); project.resync is
// enqueued by TriggerResync, which backs POST
// /linked-gitlab-projects/{linkID}/sync-runs.
package projectsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Job kinds this package handles, registered into an internal/sync.Registry
// by internal/http wiring (cmd/api/main.go).
const (
	KindProjectImport = "project.import"
	KindProjectResync = "project.resync"
)

// gitlab_sync_runs.kind values (the migration's CHECK constraint), distinct
// from the sync_jobs.kind constants above: one run records one job
// execution, but the two vocabularies serve different tables and are not
// meant to be conflated.
const (
	RunKindInitialImport = "initial_import"
	RunKindManualResync  = "manual_resync"
)

// linked_gitlab_projects.initial_import_status values internal/projectsync
// writes. "pending" is the column's default, set at link creation and never
// written here.
const (
	InitialImportStatusCompleted = "completed"
	InitialImportStatusFailed    = "failed"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes.
var (
	ErrNotFound      = errors.New("projectsync: not found")
	ErrRunInProgress = errors.New("projectsync: a sync run is already in progress for this linked project")
)

// RunPayload is the sync_jobs.payload shape for both KindProjectImport and
// KindProjectResync.
type RunPayload struct {
	SyncRunID uuid.UUID `json:"syncRunId"`
	// Full, when true, re-fetches every issue regardless of when it was
	// last updated. When false, only issues updated since the linked
	// project's last successful sync are fetched (GitLab's updated_after
	// filter) — or every issue, if there is no previous sync to diff
	// against. EnqueueImport always sets this true: an initial import has
	// nothing to diff against yet.
	Full bool `json:"full"`
}

// SyncRun is the API-facing representation of one gitlab_sync_runs row.
type SyncRun struct {
	ID                    uuid.UUID  `json:"id"`
	LinkedGitlabProjectID uuid.UUID  `json:"linkedGitlabProjectId"`
	Kind                  string     `json:"kind"`
	Status                string     `json:"status"`
	IssuesSeen            int32      `json:"issuesSeen"`
	IssuesCreated         int32      `json:"issuesCreated"`
	IssuesUpdated         int32      `json:"issuesUpdated"`
	StartedAt             *time.Time `json:"startedAt,omitempty"`
	CompletedAt           *time.Time `json:"completedAt,omitempty"`
	ErrorMessage          string     `json:"errorMessage,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
}

func fromRunRow(row db.GitlabSyncRun) SyncRun {
	sr := SyncRun{
		ID:                    row.ID,
		LinkedGitlabProjectID: row.LinkedGitlabProjectID,
		Kind:                  row.Kind,
		Status:                row.Status,
		IssuesSeen:            row.IssuesSeen,
		IssuesCreated:         row.IssuesCreated,
		IssuesUpdated:         row.IssuesUpdated,
		ErrorMessage:          row.ErrorMessage,
		CreatedAt:             row.CreatedAt.Time,
	}
	if row.StartedAt.Valid {
		t := row.StartedAt.Time
		sr.StartedAt = &t
	}
	if row.CompletedAt.Valid {
		t := row.CompletedAt.Time
		sr.CompletedAt = &t
	}
	return sr
}

// Service triggers and lists sync runs (HTTP-facing) and executes them
// (the background worker, like internal/issuesync's Service). Its queries
// are unscoped except where an ownerID parameter says otherwise: a
// worker-driven call has no acting user, the same reasoning as
// internal/issuesync and internal/webhookapply.
type Service struct {
	q             db.Querier
	txRunner      database.TxRunner
	cipher        *crypto.Cipher
	clientFactory gitlabconn.ClientFactory
}

// NewService constructs a Service. clientFactory builds the gitlab.Client
// used to call a linked project's GitLab instance; cipher decrypts its
// connection's stored access token.
func NewService(q db.Querier, txRunner database.TxRunner, cipher *crypto.Cipher, clientFactory gitlabconn.ClientFactory) *Service {
	return &Service{q: q, txRunner: txRunner, cipher: cipher, clientFactory: clientFactory}
}

// createRunAndEnqueue records a new gitlab_sync_runs row and enqueues the
// sync_jobs row that executes it, both through q so a caller can make the
// pair atomic via a TxRunner (see EnqueueImport and TriggerResync): if the
// enqueue half failed after the run row committed, the run would stay
// 'running' forever, and the partial UNIQUE index on gitlab_sync_runs would
// then reject every future sync attempt for this link as already in
// progress.
func createRunAndEnqueue(ctx context.Context, q db.Querier, linkID, appProjectID uuid.UUID, runKind, jobKind string, full bool) (db.GitlabSyncRun, error) {
	run, err := q.CreateGitlabSyncRun(ctx, db.CreateGitlabSyncRunParams{LinkedGitlabProjectID: linkID, Kind: runKind})
	if err != nil {
		if isUniqueViolation(err) {
			return db.GitlabSyncRun{}, ErrRunInProgress
		}
		return db.GitlabSyncRun{}, fmt.Errorf("projectsync: create run: %w", err)
	}

	payload, err := json.Marshal(RunPayload{SyncRunID: run.ID, Full: full})
	if err != nil {
		return db.GitlabSyncRun{}, fmt.Errorf("projectsync: encode payload: %w", err)
	}
	if _, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: appProjectID,
		Kind:      jobKind,
		Payload:   payload,
	}); err != nil {
		return db.GitlabSyncRun{}, fmt.Errorf("projectsync: enqueue job: %w", err)
	}
	return run, nil
}

// EnqueueImport starts a project.import run for a freshly linked GitLab
// project: internal/linkedproject.Service.Create calls this right after
// inserting the link row. linkedGitlabProjectID must not already have a run
// in flight (it was just created), so ErrRunInProgress should never occur
// in practice; Create treats any error from this as best-effort (logged,
// not failed), the same as it already does for webhook registration — the
// link stays usable via manual resync either way.
func EnqueueImport(ctx context.Context, txRunner database.TxRunner, linkedGitlabProjectID, appProjectID uuid.UUID) (db.GitlabSyncRun, error) {
	var run db.GitlabSyncRun
	err := txRunner.RunInTx(ctx, func(q db.Querier) error {
		var err error
		run, err = createRunAndEnqueue(ctx, q, linkedGitlabProjectID, appProjectID, RunKindInitialImport, KindProjectImport, true)
		return err
	})
	if err != nil {
		return db.GitlabSyncRun{}, err
	}
	return run, nil
}

// TriggerResync starts a project.resync run for linkID, scoped to ownerID.
// It returns ErrNotFound if linkID does not exist or belongs to another
// user, and ErrRunInProgress if a sync run for linkID is already running
// (POST /linked-gitlab-projects/{linkID}/sync-runs maps that to HTTP 409).
func (s *Service) TriggerResync(ctx context.Context, ownerID, linkID uuid.UUID, full bool) (SyncRun, error) {
	link, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: linkID, OwnerUserID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SyncRun{}, ErrNotFound
		}
		return SyncRun{}, fmt.Errorf("projectsync: trigger resync: %w", err)
	}
	conn, err := s.q.GetGitlabConnectionByID(ctx, link.GitlabConnectionID)
	if err != nil {
		return SyncRun{}, fmt.Errorf("projectsync: trigger resync: get connection: %w", err)
	}

	var run db.GitlabSyncRun
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		var err error
		run, err = createRunAndEnqueue(ctx, q, linkID, conn.ProjectID, RunKindManualResync, KindProjectResync, full)
		return err
	})
	if err != nil {
		return SyncRun{}, err
	}
	return fromRunRow(run), nil
}

// ListRuns returns linkID's sync run history, newest first, scoped to
// ownerID. It returns ErrNotFound if linkID does not exist or belongs to
// another user.
func (s *Service) ListRuns(ctx context.Context, ownerID, linkID uuid.UUID) ([]SyncRun, error) {
	if _, err := s.q.GetLinkedGitlabProjectForOwner(ctx, db.GetLinkedGitlabProjectForOwnerParams{ID: linkID, OwnerUserID: ownerID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("projectsync: list runs: %w", err)
	}

	rows, err := s.q.ListGitlabSyncRunsByLinkedGitlabProjectID(ctx, linkID)
	if err != nil {
		return nil, fmt.Errorf("projectsync: list runs: %w", err)
	}
	out := make([]SyncRun, len(rows))
	for i, row := range rows {
		out[i] = fromRunRow(row)
	}
	return out, nil
}

// HandleImport executes a project.import job. It is registered into an
// internal/sync.Registry under KindProjectImport.
func (s *Service) HandleImport(ctx context.Context, job db.SyncJob) error {
	return s.handleRun(ctx, job)
}

// HandleResync executes a project.resync job. It is registered into an
// internal/sync.Registry under KindProjectResync.
func (s *Service) HandleResync(ctx context.Context, job db.SyncJob) error {
	return s.handleRun(ctx, job)
}

// handleRun walks every page of the run's linked GitLab project's issues
// (or, for a diff resync, every page updated since the link's last sync)
// and applies each in-scope one via applyIssue, then records the outcome on
// the gitlab_sync_runs row. A failure at any point — dialing GitLab, a
// paginated list call, or applying one issue — ends the run as 'failed'
// with whatever counts were reached so far; it is never retried by
// internal/sync's generic backoff, since gitlab_sync_runs.status is already
// this run's terminal record (see failRun's doc comment). The sync_jobs row
// itself therefore always "succeeds": its one job is to run the import once
// and record what happened, and it did that.
func (s *Service) handleRun(ctx context.Context, job db.SyncJob) error {
	var payload RunPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("projectsync: decode payload: %w", err)
	}

	run, err := s.q.GetGitlabSyncRunByID(ctx, payload.SyncRunID)
	if err != nil {
		return fmt.Errorf("projectsync: get run %s: %w", payload.SyncRunID, err)
	}

	link, err := s.q.GetLinkedGitlabProjectByID(ctx, run.LinkedGitlabProjectID)
	if err != nil {
		return fmt.Errorf("projectsync: get linked project: %w", err)
	}

	conn, err := s.q.GetGitlabConnectionByID(ctx, link.GitlabConnectionID)
	if err != nil {
		return s.failRun(ctx, run, err)
	}
	token, err := s.cipher.Decrypt(conn.EncryptedToken)
	if err != nil {
		return s.failRun(ctx, run, fmt.Errorf("decrypt token: %w", err))
	}
	project, err := s.q.GetProjectByID(ctx, conn.ProjectID)
	if err != nil {
		return s.failRun(ctx, run, fmt.Errorf("get project: %w", err))
	}
	client := s.clientFactory(conn.BaseUrl)

	var updatedAfter *time.Time
	if !payload.Full && link.LastSyncedAt.Valid {
		t := link.LastSyncedAt.Time
		updatedAfter = &t
	}

	var seen, created, updated int32
	for page := 1; page != 0; {
		issues, pageInfo, err := client.ListIssues(ctx, token, link.GitlabProjectID, gitlab.ListIssuesOptions{
			ListOptions:  gitlab.ListOptions{Page: page, PerPage: 50},
			UpdatedAfter: updatedAfter,
		})
		if err != nil {
			return s.failRunWithCounts(ctx, run, seen, created, updated, fmt.Errorf("list issues (page %d): %w", page, err))
		}

		for _, issue := range issues {
			if !inScope(issue.Labels, link.SyncScope, link.SyncLabels) {
				continue
			}
			seen++
			outcome, err := s.applyIssue(ctx, link, project, issue)
			if err != nil {
				return s.failRunWithCounts(ctx, run, seen, created, updated, fmt.Errorf("apply issue !%d: %w", issue.IID, err))
			}
			switch outcome {
			case applyCreated:
				created++
			case applyUpdated:
				updated++
			}
		}

		page = pageInfo.NextPage
	}

	return s.completeRun(ctx, run, link, seen, created, updated)
}

// applyOutcome reports what applyIssue did with one issue, for handleRun's
// created/updated counters.
type applyOutcome int

const (
	applySkipped applyOutcome = iota
	applyCreated
	applyUpdated
)

// applyIssue tells a known issue (a task already linked to it) from an
// unknown one, the same lookup internal/webhookapply's inbound pipeline
// uses, and applies it accordingly.
func (s *Service) applyIssue(ctx context.Context, link db.LinkedGitlabProject, project db.Project, issue gitlab.Issue) (applyOutcome, error) {
	fields := fieldsFromIssue(issue)

	existing, err := s.q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
		LinkedGitlabProjectID: link.ID,
		GitlabIssueIid:        fields.IssueIID,
	})
	switch {
	case err == nil:
		return s.applyToExistingTask(ctx, existing, fields)
	case errors.Is(err, pgx.ErrNoRows):
		return s.applyAsNewTask(ctx, link, project, fields)
	default:
		return applySkipped, fmt.Errorf("get task link: %w", err)
	}
}

// applyToExistingTask reuses the exact stale guard internal/webhookapply's
// applyToExistingTask uses: an issue whose updated_at is strictly older
// than the last GitLab state we applied is skipped, so a resync (or a
// re-run of an import) never overwrites a task with stale data — in
// particular, it never clobbers a change the app side made more recently
// than GitLab last told us about (docs/plans/issue-sync.md's stale guard).
// A second run over an already-imported issue always lands here, which is
// what makes import idempotent: re-running it creates no duplicate tasks.
func (s *Service) applyToExistingTask(ctx context.Context, link db.TaskGitlabLink, fields issueFields) (applyOutcome, error) {
	if !fields.UpdatedAt.IsZero() && link.GitlabUpdatedAt.Valid && fields.UpdatedAt.Before(link.GitlabUpdatedAt.Time) {
		return applySkipped, nil
	}

	if _, err := s.q.ApplyWebhookTaskFields(ctx, db.ApplyWebhookTaskFieldsParams{
		ID:                     link.TaskID,
		Title:                  fields.Title,
		Description:            fields.Description,
		AssigneeGitlabUserID:   toInt8(fields.AssigneeID),
		AssigneeGitlabUsername: fields.AssigneeUsername,
		Labels:                 fields.Labels,
		DueOn:                  toDate(fields.DueDate),
		Status:                 fields.Status,
	}); err != nil {
		return applySkipped, fmt.Errorf("update task: %w", err)
	}
	if _, err := s.q.MarkTaskGitlabLinkAppliedForTask(ctx, db.MarkTaskGitlabLinkAppliedForTaskParams{
		TaskID:          link.TaskID,
		GitlabUpdatedAt: toTimestamptz(fields.UpdatedAt),
	}); err != nil {
		return applySkipped, fmt.Errorf("mark link applied: %w", err)
	}
	return applyUpdated, nil
}

// applyAsNewTask creates a new unclassified task (backlog_id = NULL) and
// links it to fields' issue, the same shape as
// internal/webhookapply.applyAsNewTask. The create-task-then-link pair runs
// in its own transaction (unlike applyToExistingTask's two writes, which
// don't need one — see handleRun's doc comment on retry safety) because a
// concurrent webhook delivery for the same not-yet-imported issue can win
// the UNIQUE (linked_gitlab_project_id, gitlab_issue_iid) race on the link
// insert; without a transaction the task created just above would be left
// behind as an orphan duplicate.
func (s *Service) applyAsNewTask(ctx context.Context, link db.LinkedGitlabProject, project db.Project, fields issueFields) (applyOutcome, error) {
	outcome := applySkipped
	err := s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		taskRow, err := q.CreateTask(ctx, db.CreateTaskParams{
			ProjectID:              project.ID,
			Title:                  fields.Title,
			Description:            fields.Description,
			AssigneeGitlabUserID:   toInt8(fields.AssigneeID),
			AssigneeGitlabUsername: fields.AssigneeUsername,
			Labels:                 fields.Labels,
			DueOn:                  toDate(fields.DueDate),
			Priority:               task.PriorityMedium,
			Progress:               task.ProgressNotStarted,
			CreatedByUserID:        project.OwnerUserID,
		})
		if err != nil {
			return fmt.Errorf("create task: %w", err)
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
				return fmt.Errorf("close new task: %w", err)
			}
		}

		// A UNIQUE violation here rolls back the task just created above and
		// is returned as-is (not wrapped, not marked failed): it means a
		// concurrent webhook apply already linked this issue first, so this
		// attempt is a no-op, not a failure.
		if _, err := q.CreateTaskGitlabLink(ctx, db.CreateTaskGitlabLinkParams{
			TaskID:                taskRow.ID,
			LinkedGitlabProjectID: link.ID,
			GitlabIssueID:         fields.IssueID,
			GitlabIssueIid:        fields.IssueIID,
			GitlabWebUrl:          fields.WebURL,
			GitlabUpdatedAt:       toTimestamptz(fields.UpdatedAt),
			LastPushedFingerprint: fields.Fingerprint,
		}); err != nil {
			return err
		}
		outcome = applyCreated
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return applySkipped, nil
		}
		return applySkipped, fmt.Errorf("projectsync: apply as new task: %w", err)
	}
	return outcome, nil
}

// completeRun records a successful run and refreshes the link's
// last_synced_at (and, for an initial import, its initial_import_status).
func (s *Service) completeRun(ctx context.Context, run db.GitlabSyncRun, link db.LinkedGitlabProject, seen, created, updated int32) error {
	if _, err := s.q.CompleteGitlabSyncRun(ctx, db.CompleteGitlabSyncRunParams{
		ID:            run.ID,
		IssuesSeen:    seen,
		IssuesCreated: created,
		IssuesUpdated: updated,
	}); err != nil {
		return fmt.Errorf("projectsync: complete run: %w", err)
	}
	if _, err := s.q.UpdateLinkedGitlabProjectLastSyncedAt(ctx, link.ID); err != nil {
		return fmt.Errorf("projectsync: update last synced at: %w", err)
	}
	if run.Kind == RunKindInitialImport {
		if _, err := s.q.UpdateLinkedGitlabProjectInitialImportStatus(ctx, db.UpdateLinkedGitlabProjectInitialImportStatusParams{
			ID:                  link.ID,
			InitialImportStatus: InitialImportStatusCompleted,
		}); err != nil {
			return fmt.Errorf("projectsync: update initial import status: %w", err)
		}
	}
	return nil
}

// failRun and failRunWithCounts record a run failure on gitlab_sync_runs
// (and, for an initial import, on the link's initial_import_status), then
// return nil rather than cause: gitlab_sync_runs.status is this run's
// terminal record of success or failure, not internal/sync's generic
// retry/backoff, so handleRun's caller (the sync.Worker) must not retry —
// that would re-run the whole import from page 1 and, worse, could
// eventually mark a run 'succeeded' whose gitlab_sync_runs row a concurrent
// caller already observed as 'failed' and moved past.
func (s *Service) failRun(ctx context.Context, run db.GitlabSyncRun, cause error) error {
	return s.failRunWithCounts(ctx, run, 0, 0, 0, cause)
}

func (s *Service) failRunWithCounts(ctx context.Context, run db.GitlabSyncRun, seen, created, updated int32, cause error) error {
	if _, err := s.q.FailGitlabSyncRun(ctx, db.FailGitlabSyncRunParams{
		ID:            run.ID,
		IssuesSeen:    seen,
		IssuesCreated: created,
		IssuesUpdated: updated,
		ErrorMessage:  cause.Error(),
	}); err != nil {
		return fmt.Errorf("projectsync: fail run: %w (after: %v)", err, cause)
	}
	if run.Kind == RunKindInitialImport {
		if _, err := s.q.UpdateLinkedGitlabProjectInitialImportStatus(ctx, db.UpdateLinkedGitlabProjectInitialImportStatusParams{
			ID:                  run.LinkedGitlabProjectID,
			InitialImportStatus: InitialImportStatusFailed,
		}); err != nil {
			return fmt.Errorf("projectsync: update initial import status: %w (after: %v)", err, cause)
		}
	}
	return nil
}

// issueFields is a gitlab.Issue's fields, normalized into the shape both
// the scope check and the tasks/task_gitlab_links writes need — the import
// pipeline's equivalent of internal/webhookapply's taskFields.
type issueFields struct {
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

func fieldsFromIssue(issue gitlab.Issue) issueFields {
	var assigneeID *int64
	var assigneeUsername string
	var assigneeIDs []int64
	if len(issue.Assignees) > 0 {
		id := issue.Assignees[0].ID
		assigneeID = &id
		assigneeUsername = issue.Assignees[0].Username
		assigneeIDs = []int64{id}
	}

	dueDate := parseGitlabDate(issue.DueDate)

	status := task.StatusOpen
	if issue.State == "closed" {
		status = task.StatusClosed
	}

	// tasks.labels is NOT NULL; a GitLab issue with no labels reports a nil
	// slice, which pgx would otherwise bind as SQL NULL and fail CreateTask /
	// ApplyWebhookTaskFields (internal/task.Service.Create and
	// internal/webhookapply.fieldsFromPayload normalize the same way).
	labels := issue.Labels
	if labels == nil {
		labels = []string{}
	}

	return issueFields{
		Title:            issue.Title,
		Description:      issue.Description,
		Status:           status,
		Labels:           labels,
		DueDate:          dueDate,
		AssigneeID:       assigneeID,
		AssigneeUsername: assigneeUsername,
		UpdatedAt:        issue.UpdatedAt,
		IssueID:          issue.ID,
		IssueIID:         issue.IID,
		WebURL:           issue.WebURL,
		Fingerprint:      gitlab.Fingerprint(issue.Title, issue.Description, labels, dueDate, assigneeIDs),
	}
}

// inScope mirrors linkedproject.InScope exactly (issue labels are in scope
// under "all", or under "labels" when at least one matches syncLabels). It
// is duplicated here rather than imported: internal/linkedproject calls
// EnqueueImport (above) when a link is created, so this package must not
// import internal/linkedproject back.
func inScope(issueLabels []string, scope string, syncLabels []string) bool {
	if scope != "labels" {
		return true
	}
	for _, want := range syncLabels {
		if slices.Contains(issueLabels, want) {
			return true
		}
	}
	return false
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

// parseGitlabDate parses a GitLab issue's due_date ("YYYY-MM-DD"). An empty
// or unparsable value returns nil.
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

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
