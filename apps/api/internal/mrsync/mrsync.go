// Package mrsync implements the mr.import and mr.resync sync_jobs handlers
// (issue #111, building on ADR-0011): walking every page of a repository's
// GitLab merge requests (and, per MR, its current head pipeline and review
// notes) and creating or updating the app's merge_requests rows from them,
// recording each attempt as a repository_sync_runs row — the same shape
// internal/projectsync already uses for issue sync, scoped to a Repository
// instead of a LinkedGitlabProject. mr.import is enqueued automatically when
// a GitLab project is linked (see EnsureRepository/EnqueueImport, called
// from internal/linkedproject.Service.Create), right alongside issue sync's
// own project.import.
//
// This is read-only: FlowLens never writes a merge request back to GitLab
// (ADR-0011's premise, unlike issue sync's two-way push/pull).
package mrsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Job kinds this package handles, registered into an internal/sync.Registry
// by internal/http wiring (cmd/api/main.go).
const (
	KindMRImport = "mr.import"
	KindMRResync = "mr.resync"
)

// repository_sync_runs.kind values (the migration's CHECK constraint),
// mirroring projectsync.RunKindInitialImport/RunKindManualResync.
const (
	RunKindInitialImport = "initial_import"
	RunKindManualResync  = "manual_resync"
)

// ErrRunInProgress is returned by EnqueueImport/EnqueueResync when a run is
// already in flight for the same repository.
var ErrRunInProgress = errors.New("mrsync: a sync run is already in progress for this repository")

// RunPayload is the sync_jobs.payload shape for both KindMRImport and
// KindMRResync, the same shape as projectsync.RunPayload.
type RunPayload struct {
	SyncRunID uuid.UUID `json:"syncRunId"`
	// Full, when true, re-fetches every merge request regardless of when it
	// was last updated. EnqueueImport always sets this true.
	Full bool `json:"full"`
}

// Service executes mr.import/mr.resync jobs (the background worker). Its
// queries are unscoped: like internal/projectsync's worker half, a job row
// has no acting user of its own — it was already authorized when it was
// created (automatically, alongside the repository, at link creation).
type Service struct {
	q             db.Querier
	txRunner      database.TxRunner
	cipher        *crypto.Cipher
	clientFactory gitlabconn.ClientFactory
}

// NewService constructs a Service. clientFactory builds the gitlab.Client
// used to call a repository's GitLab instance; cipher decrypts its linked
// project's connection's stored access token.
func NewService(q db.Querier, txRunner database.TxRunner, cipher *crypto.Cipher, clientFactory gitlabconn.ClientFactory) *Service {
	return &Service{q: q, txRunner: txRunner, cipher: cipher, clientFactory: clientFactory}
}

// EnsureRepository creates link's Repository row (the MR-tracking sibling of
// a linked GitLab project, ADR-0011 §1) if it does not already exist, and
// returns it either way. Called from internal/linkedproject.Service.Create
// right after the link itself is created — one linked_gitlab_projects row
// has at most one repositories row, enforced by the UNIQUE constraint, so a
// concurrent duplicate call is a benign no-op.
func EnsureRepository(ctx context.Context, q db.Querier, link db.LinkedGitlabProject) (db.Repository, error) {
	existing, err := q.GetRepositoryByLinkedGitlabProjectID(ctx, link.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, fmt.Errorf("mrsync: get repository: %w", err)
	}

	row, err := q.CreateRepository(ctx, db.CreateRepositoryParams{
		LinkedGitlabProjectID: link.ID,
		Name:                  link.Name,
		FullName:              link.PathWithNamespace,
		DefaultBranch:         "main",
		HtmlUrl:               link.WebUrl,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return q.GetRepositoryByLinkedGitlabProjectID(ctx, link.ID)
		}
		return db.Repository{}, fmt.Errorf("mrsync: create repository: %w", err)
	}
	return row, nil
}

// createRunAndEnqueue mirrors projectsync's function of the same name,
// scoped to a repository instead of a linked GitLab project.
func createRunAndEnqueue(ctx context.Context, q db.Querier, repositoryID, appProjectID uuid.UUID, runKind, jobKind string, full bool) (db.RepositorySyncRun, error) {
	run, err := q.CreateRepositorySyncRun(ctx, db.CreateRepositorySyncRunParams{RepositoryID: repositoryID, Kind: runKind})
	if err != nil {
		if isUniqueViolation(err) {
			return db.RepositorySyncRun{}, ErrRunInProgress
		}
		return db.RepositorySyncRun{}, fmt.Errorf("mrsync: create run: %w", err)
	}

	payload, err := json.Marshal(RunPayload{SyncRunID: run.ID, Full: full})
	if err != nil {
		return db.RepositorySyncRun{}, fmt.Errorf("mrsync: encode payload: %w", err)
	}
	if _, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: appProjectID,
		Kind:      jobKind,
		Payload:   payload,
	}); err != nil {
		return db.RepositorySyncRun{}, fmt.Errorf("mrsync: enqueue job: %w", err)
	}
	return run, nil
}

// EnqueueImport starts an mr.import run for a freshly created repository:
// internal/linkedproject.Service.Create calls this right after
// EnsureRepository. repositoryID must not already have a run in flight (it
// was just created), so ErrRunInProgress should never occur in practice;
// Create treats any error from this as best-effort (logged, not failed),
// the same as it already does for issue sync's own EnqueueImport.
func EnqueueImport(ctx context.Context, txRunner database.TxRunner, repositoryID, appProjectID uuid.UUID) (db.RepositorySyncRun, error) {
	var run db.RepositorySyncRun
	err := txRunner.RunInTx(ctx, func(q db.Querier) error {
		var err error
		run, err = createRunAndEnqueue(ctx, q, repositoryID, appProjectID, RunKindInitialImport, KindMRImport, true)
		return err
	})
	if err != nil {
		return db.RepositorySyncRun{}, err
	}
	return run, nil
}

// HandleImport executes an mr.import job. It is registered into an
// internal/sync.Registry under KindMRImport.
func (s *Service) HandleImport(ctx context.Context, job db.SyncJob) error {
	return s.handleRun(ctx, job)
}

// HandleResync executes an mr.resync job. It is registered into an
// internal/sync.Registry under KindMRResync.
func (s *Service) HandleResync(ctx context.Context, job db.SyncJob) error {
	return s.handleRun(ctx, job)
}

// handleRun walks every page of the run's repository's merge requests (or,
// for a diff resync, every page updated since the repository's last sync)
// and applies each one via applyMergeRequest, then records the outcome on
// the repository_sync_runs row. Like projectsync.handleRun, a failure ends
// the run as 'failed' with whatever counts were reached so far and always
// returns nil, so internal/sync's generic retry never re-runs the whole
// import — repository_sync_runs.status is already this run's terminal
// record.
func (s *Service) handleRun(ctx context.Context, job db.SyncJob) error {
	var payload RunPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("mrsync: decode payload: %w", err)
	}

	run, err := s.q.GetRepositorySyncRunByID(ctx, payload.SyncRunID)
	if err != nil {
		return fmt.Errorf("mrsync: get run %s: %w", payload.SyncRunID, err)
	}

	repo, err := s.q.GetRepositoryByID(ctx, run.RepositoryID)
	if err != nil {
		return fmt.Errorf("mrsync: get repository: %w", err)
	}
	link, err := s.q.GetLinkedGitlabProjectByID(ctx, repo.LinkedGitlabProjectID)
	if err != nil {
		return s.failRun(ctx, run, err)
	}
	conn, err := s.q.GetGitlabConnectionByID(ctx, link.GitlabConnectionID)
	if err != nil {
		return s.failRun(ctx, run, err)
	}
	token, err := s.cipher.Decrypt(conn.EncryptedToken)
	if err != nil {
		return s.failRun(ctx, run, fmt.Errorf("decrypt token: %w", err))
	}
	client := s.clientFactory(conn.BaseUrl)

	// repositories has no last-synced-at column of its own yet: only Full
	// imports are triggered today (EnqueueImport always sets it), so there is
	// nothing to diff a partial resync against. A future mr.resync trigger
	// (not part of issue #111's scope) would need one, the same role
	// linked_gitlab_projects.last_synced_at plays for issue sync.
	var updatedAfter *time.Time

	var seen, created, updated int32
	for page := 1; page != 0; {
		mrs, pageInfo, err := client.ListMergeRequests(ctx, token, link.GitlabProjectID, gitlab.ListMergeRequestsOptions{
			ListOptions:  gitlab.ListOptions{Page: page, PerPage: 50},
			UpdatedAfter: updatedAfter,
			State:        "all",
		})
		if err != nil {
			return s.failRunWithCounts(ctx, run, seen, created, updated, fmt.Errorf("list merge requests (page %d): %w", page, err))
		}

		for _, mr := range mrs {
			seen++
			full, err := client.GetMergeRequest(ctx, token, link.GitlabProjectID, mr.IID)
			if err != nil {
				return s.failRunWithCounts(ctx, run, seen, created, updated, fmt.Errorf("get merge request !%d: %w", mr.IID, err))
			}
			outcome, err := s.applyMergeRequest(ctx, repo, link, client, token, *full)
			if err != nil {
				return s.failRunWithCounts(ctx, run, seen, created, updated, fmt.Errorf("apply merge request !%d: %w", mr.IID, err))
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

	return s.completeRun(ctx, run, seen, created, updated)
}

type applyOutcome int

const (
	applySkipped applyOutcome = iota
	applyCreated
	applyUpdated
)

// applyMergeRequest tells a known merge request (already imported) from an
// unknown one, and applies it accordingly — the same known/unknown split
// projectsync.applyIssue uses for issues.
func (s *Service) applyMergeRequest(ctx context.Context, repo db.Repository, link db.LinkedGitlabProject, client gitlab.Client, token string, mr gitlab.MergeRequest) (applyOutcome, error) {
	fields := fieldsFromMergeRequest(repo.ID, mr)

	existing, err := s.q.GetMergeRequestByGitlabMergeRequestID(ctx, fields.GitlabMergeRequestID)
	switch {
	case err == nil:
		return s.applyToExistingMergeRequest(ctx, existing, link, client, token, mr, fields)
	case errors.Is(err, pgx.ErrNoRows):
		return s.applyAsNewMergeRequest(ctx, link, client, token, mr, fields)
	default:
		return applySkipped, fmt.Errorf("get merge request: %w", err)
	}
}

// applyToExistingMergeRequest applies the same stale guard
// projectsync/webhookapply use for issues: a merge request whose updated_at
// is no newer than what we already applied is a no-op, which is what makes
// import idempotent — re-running it creates no duplicate rows.
func (s *Service) applyToExistingMergeRequest(ctx context.Context, existing db.MergeRequest, link db.LinkedGitlabProject, client gitlab.Client, token string, mr gitlab.MergeRequest, fields mrFields) (applyOutcome, error) {
	if !fields.UpdatedAt.IsZero() && existing.GitlabUpdatedAt.Valid && !fields.UpdatedAt.After(existing.GitlabUpdatedAt.Time) {
		s.enrich(ctx, existing, link, client, token, mr)
		return applySkipped, nil
	}

	updated, err := s.q.UpdateMergeRequest(ctx, db.UpdateMergeRequestParams{
		GitlabMergeRequestID: fields.GitlabMergeRequestID,
		Title:                fields.Title,
		State:                fields.State,
		IsDraft:              fields.IsDraft,
		AuthorGitlabUsername: fields.AuthorUsername,
		AuthorAvatarUrl:      fields.AuthorAvatarURL,
		BaseBranch:           fields.TargetBranch,
		HeadBranch:           fields.SourceBranch,
		GitlabUpdatedAt:      toTimestamptz(fields.UpdatedAt),
		MergedAt:             toTimestamptzPtr(fields.MergedAt),
		ClosedAt:             toTimestamptzPtr(fields.ClosedAt),
		HtmlUrl:              fields.WebURL,
		PipelineStatus:       fields.PipelineStatus,
		PipelineID:           fields.PipelineID,
		PipelineUpdatedAt:    fields.PipelineUpdatedAt,
	})
	if err != nil {
		return applySkipped, fmt.Errorf("update merge request: %w", err)
	}
	s.enrich(ctx, updated, link, client, token, mr)
	return applyUpdated, nil
}

// applyAsNewMergeRequest inserts a new merge_requests row. Unlike issue
// sync's applyAsNewTask, this needs no transaction: a merge request row has
// no dependent rows created alongside it, so the UNIQUE constraint on
// gitlab_merge_request_id alone is enough idempotency — a concurrent insert
// losing the race is a benign no-op, not a failure.
func (s *Service) applyAsNewMergeRequest(ctx context.Context, link db.LinkedGitlabProject, client gitlab.Client, token string, mr gitlab.MergeRequest, fields mrFields) (applyOutcome, error) {
	row, err := s.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID:         fields.RepositoryID,
		GitlabMergeRequestID: fields.GitlabMergeRequestID,
		Number:               fields.Number,
		Title:                fields.Title,
		State:                fields.State,
		IsDraft:              fields.IsDraft,
		AuthorGitlabUsername: fields.AuthorUsername,
		AuthorAvatarUrl:      fields.AuthorAvatarURL,
		BaseBranch:           fields.TargetBranch,
		HeadBranch:           fields.SourceBranch,
		GitlabCreatedAt:      toTimestamptz(fields.CreatedAt),
		GitlabUpdatedAt:      toTimestamptz(fields.UpdatedAt),
		MergedAt:             toTimestamptzPtr(fields.MergedAt),
		ClosedAt:             toTimestamptzPtr(fields.ClosedAt),
		HtmlUrl:              fields.WebURL,
		PipelineStatus:       fields.PipelineStatus,
		PipelineID:           fields.PipelineID,
		PipelineUpdatedAt:    fields.PipelineUpdatedAt,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return applySkipped, nil
		}
		return applySkipped, fmt.Errorf("create merge request: %w", err)
	}
	s.enrich(ctx, row, link, client, token, mr)
	return applyCreated, nil
}

// enrich sets a merge request's first_reviewed_at (at most once, ADR-0011
// §3) and task_id (issue #111's task-linking requirement) if not already
// set. Failures here are logged-and-ignored by the caller's perspective —
// enrich itself swallows them (returning nothing) because a missing review
// signal or task link must never fail the whole sync run; a later resync
// will simply try again.
func (s *Service) enrich(ctx context.Context, row db.MergeRequest, link db.LinkedGitlabProject, client gitlab.Client, token string, mr gitlab.MergeRequest) {
	if !row.FirstReviewedAt.Valid {
		notes, _, err := client.ListMergeRequestNotes(ctx, token, link.GitlabProjectID, mr.IID, gitlab.ListOptions{PerPage: 100})
		if err == nil {
			if t := firstReviewActivity(notes, mr.Author.ID); t != nil {
				_, _ = s.q.UpdateMergeRequestFirstReviewedAt(ctx, db.UpdateMergeRequestFirstReviewedAtParams{
					ID:              row.ID,
					FirstReviewedAt: toTimestamptz(*t),
				})
			}
		}
	}

	if !row.TaskID.Valid {
		if iid, ok := parseIssueIID(mr.Description, mr.SourceBranch); ok {
			taskLink, err := s.q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
				LinkedGitlabProjectID: link.ID,
				GitlabIssueIid:        iid,
			})
			if err == nil {
				_, _ = s.q.UpdateMergeRequestTaskID(ctx, db.UpdateMergeRequestTaskIDParams{
					ID:     row.ID,
					TaskID: pgtype.UUID{Bytes: taskLink.TaskID, Valid: true},
				})
			}
		}
	}
}

// closesRe matches GitLab's issue-closing keywords ("Closes #12", "fixes
// #12", "Resolved #12", ...) in a merge request's description.
var closesRe = regexp.MustCompile(`(?i)\b(?:clos(?:e|es|ed)|fix(?:es|ed)?|resolv(?:e|es|ed))\s+#(\d+)\b`)

// branchIssueRe matches a leading or path-segment issue number in a branch
// name ("42-fix-thing", "issue-42", "feature/42-foo").
var branchIssueRe = regexp.MustCompile(`(?:^|/)(?:issue-)?(\d+)(?:-|$)`)

// parseIssueIID extracts a referenced issue IID from a merge request's
// description (preferred: an explicit closing keyword) or, failing that,
// its source branch name (issue #111's task-linking requirement). It
// returns false when neither yields a plausible issue reference.
func parseIssueIID(description, sourceBranch string) (int64, bool) {
	if m := closesRe.FindStringSubmatch(description); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return n, true
		}
	}
	if m := branchIssueRe.FindStringSubmatch(sourceBranch); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// firstReviewActivity returns the earliest note timestamp that plausibly
// represents review activity: any note from someone other than the MR's
// author, excluding GitLab's own bookkeeping system notes (assigned,
// labeled, ...) except an approval note. GitLab CE's approvals endpoint
// carries no per-approval timestamp, so notes are the only source with one.
func firstReviewActivity(notes []gitlab.Note, authorID int64) *time.Time {
	var earliest *time.Time
	for _, n := range notes {
		if n.Author.ID == authorID {
			continue
		}
		if n.System && !strings.Contains(strings.ToLower(n.Body), "approved") {
			continue
		}
		if earliest == nil || n.CreatedAt.Before(*earliest) {
			t := n.CreatedAt
			earliest = &t
		}
	}
	return earliest
}

// completeRun records a successful run.
func (s *Service) completeRun(ctx context.Context, run db.RepositorySyncRun, seen, created, updated int32) error {
	if _, err := s.q.CompleteRepositorySyncRun(ctx, db.CompleteRepositorySyncRunParams{
		ID:         run.ID,
		MrsSeen:    seen,
		MrsCreated: created,
		MrsUpdated: updated,
	}); err != nil {
		return fmt.Errorf("mrsync: complete run: %w", err)
	}
	return nil
}

// failRun and failRunWithCounts record a run failure and return nil, the
// same "this run's terminal record is repository_sync_runs.status, not
// internal/sync's retry machinery" reasoning as projectsync.failRun.
func (s *Service) failRun(ctx context.Context, run db.RepositorySyncRun, cause error) error {
	return s.failRunWithCounts(ctx, run, 0, 0, 0, cause)
}

func (s *Service) failRunWithCounts(ctx context.Context, run db.RepositorySyncRun, seen, created, updated int32, cause error) error {
	if _, err := s.q.FailRepositorySyncRun(ctx, db.FailRepositorySyncRunParams{
		ID:           run.ID,
		MrsSeen:      seen,
		MrsCreated:   created,
		MrsUpdated:   updated,
		ErrorMessage: cause.Error(),
	}); err != nil {
		return fmt.Errorf("mrsync: fail run: %w (after: %v)", err, cause)
	}
	return nil
}

// mrFields is a gitlab.MergeRequest's fields, normalized into the shape the
// merge_requests writes need — mrsync's equivalent of projectsync.issueFields.
type mrFields struct {
	RepositoryID         uuid.UUID
	GitlabMergeRequestID int64
	Number               int32
	Title                string
	State                string
	IsDraft              bool
	AuthorUsername       string
	AuthorAvatarURL      string
	SourceBranch         string
	TargetBranch         string
	WebURL               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	MergedAt             *time.Time
	ClosedAt             *time.Time
	PipelineStatus       string
	PipelineID           pgtype.Int8
	PipelineUpdatedAt    pgtype.Timestamptz
}

func fieldsFromMergeRequest(repositoryID uuid.UUID, mr gitlab.MergeRequest) mrFields {
	f := mrFields{
		RepositoryID:         repositoryID,
		GitlabMergeRequestID: mr.ID,
		Number:               int32(mr.IID),
		Title:                mr.Title,
		State:                mr.State,
		IsDraft:              mr.Draft,
		AuthorUsername:       mr.Author.Username,
		AuthorAvatarURL:      mr.Author.AvatarURL,
		SourceBranch:         mr.SourceBranch,
		TargetBranch:         mr.TargetBranch,
		WebURL:               mr.WebURL,
		CreatedAt:            mr.CreatedAt,
		UpdatedAt:            mr.UpdatedAt,
		MergedAt:             mr.MergedAt,
		ClosedAt:             mr.ClosedAt,
	}
	if mr.HeadPipeline != nil {
		f.PipelineStatus = mr.HeadPipeline.Status
		f.PipelineID = pgtype.Int8{Int64: mr.HeadPipeline.ID, Valid: true}
		f.PipelineUpdatedAt = toTimestamptz(mr.HeadPipeline.UpdatedAt)
	}
	return f
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func toTimestamptzPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return toTimestamptz(*t)
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
