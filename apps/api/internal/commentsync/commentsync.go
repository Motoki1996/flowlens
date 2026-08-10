// Package commentsync implements the outbound sync_jobs handler that pushes
// a task's activity-log comments to their linked GitLab CE issue as notes
// (#104). It mirrors internal/issuesync's shape (dial a gitlab.Client for a
// linked project, execute one job kind), registered into the same
// internal/sync.Registry at wiring time; internal/taskcomment builds and
// enqueues the job this package executes.
package commentsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// KindCommentCreate is the sync_jobs.kind this package handles, registered
// into an internal/sync.Registry by internal/http wiring (cmd/api/main.go).
const KindCommentCreate = "comment.create"

// CreatePayload is the sync_jobs.payload shape for KindCommentCreate, built
// by internal/taskcomment.Service.
type CreatePayload struct {
	CommentID uuid.UUID `json:"commentId"`
}

// Service executes outbound comment sync jobs. Like internal/issuesync's
// Service, it runs as the background worker: the job row itself is the
// authorization (it was enqueued by an owner/token-scoped handler already),
// so its queries are unscoped.
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

// dial resolves the gitlab.Client, decrypted access token, and linked
// project row (which carries the GitLab-side numeric project ID) to call
// for linkedProjectID. Identical to internal/issuesync.Service.dial.
func (s *Service) dial(ctx context.Context, linkedProjectID uuid.UUID) (gitlab.Client, string, db.LinkedGitlabProject, error) {
	link, err := s.q.GetLinkedGitlabProjectByID(ctx, linkedProjectID)
	if err != nil {
		return nil, "", db.LinkedGitlabProject{}, fmt.Errorf("commentsync: get linked project: %w", err)
	}
	conn, err := s.q.GetGitlabConnectionByID(ctx, link.GitlabConnectionID)
	if err != nil {
		return nil, "", db.LinkedGitlabProject{}, fmt.Errorf("commentsync: get gitlab connection: %w", err)
	}
	token, err := s.cipher.Decrypt(conn.EncryptedToken)
	if err != nil {
		return nil, "", db.LinkedGitlabProject{}, fmt.Errorf("commentsync: decrypt token: %w", err)
	}
	return s.clientFactory(conn.BaseUrl), token, link, nil
}

// HandleCommentCreate pushes a task comment to its linked GitLab issue as a
// note. A task with no link is a no-op success — enqueueIfLinked in
// internal/taskcomment already only enqueues this job for a linked task, but
// the link may have been removed since. It is idempotent: a comment whose
// gitlab_note_id is already set (a prior run pushed it but the job was
// retried, e.g. because recording the note id failed) is a no-op success
// rather than posting a duplicate note.
func (s *Service) HandleCommentCreate(ctx context.Context, job db.SyncJob) error {
	taskID, err := taskIDFromJob(job)
	if err != nil {
		return err
	}

	link, err := s.q.GetTaskGitlabLinkByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("commentsync: get link: %w", err)
	}

	var payload CreatePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("commentsync: decode payload: %w", err)
	}

	comment, err := s.q.GetTaskCommentByID(ctx, payload.CommentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("commentsync: get comment: %w", err)
	}
	if comment.GitlabNoteID.Valid {
		return nil
	}

	client, token, linkedProject, err := s.dial(ctx, link.LinkedGitlabProjectID)
	if err != nil {
		return err
	}

	note, err := client.CreateNote(ctx, token, linkedProject.GitlabProjectID, link.GitlabIssueIid, gitlab.CreateNotePayload{Body: comment.Body})
	if err != nil {
		return fmt.Errorf("commentsync: create note: %w", err)
	}

	if err := s.q.SetTaskCommentGitlabNoteID(ctx, db.SetTaskCommentGitlabNoteIDParams{
		ID:           comment.ID,
		GitlabNoteID: pgtype.Int8{Int64: note.ID, Valid: true},
	}); err != nil {
		return fmt.Errorf("commentsync: save note id: %w", err)
	}
	return nil
}

func taskIDFromJob(job db.SyncJob) (uuid.UUID, error) {
	if !job.TaskID.Valid {
		return uuid.Nil, fmt.Errorf("commentsync: job %s (%s) has no task_id", job.ID, job.Kind)
	}
	return uuid.UUID(job.TaskID.Bytes), nil
}
