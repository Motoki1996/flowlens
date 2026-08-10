// Package taskcomment contains the task-comment domain model and the
// service that manages a task's activity log (issue #103): the return path
// for an AI agent that has been reading task_ai_contexts but had no way to
// report back what it did or where it got stuck. A comment is authored
// either by a logged-in user (AuthorUserID set, AuthorKind "user") or by a
// project API token (AuthorTokenID set, AuthorKind "agent") — exactly one of
// the two is ever set, the same NULL-for-the-other-side convention
// task_comments.author_user_id/author_token_id use. AuthorKind "gitlab" is
// reserved for #104's GitLab-discussion sync and is never produced here.
//
// task_comments carries no project_id of its own, the same way
// task_dependencies doesn't: ownership is always checked through the
// comment's task, via internal/task.
package taskcomment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/flowlens/api/internal/commentsync"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/project"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors returned by Service. Handlers map these to HTTP status
// codes. ErrNotFound is returned both when a comment/task does not exist and
// when it belongs to another user's project.
var (
	ErrInvalidBody = errors.New("taskcomment: body must be 1-10000 characters")
	ErrNotFound    = errors.New("taskcomment: not found")
	ErrForbidden   = errors.New("taskcomment: forbidden")
)

// maxBodyLength bounds a comment's body.
const maxBodyLength = 10000

// Author kind values, matching task_comments.author_kind's CHECK constraint.
// AuthorKindGitlab is reserved for #104 and never set by this package.
const (
	AuthorKindUser   = "user"
	AuthorKindAgent  = "agent"
	AuthorKindGitlab = "gitlab"
)

// RecentLimit bounds how many of a task's most recent comments
// GET /api/v1/tasks/{taskID}/context embeds, so an AI agent's context payload
// stays bounded the same way task_ai_contexts' own fields are length-capped.
const RecentLimit = 20

// TaskComment is the API-facing representation of one entry in a task's
// activity log.
type TaskComment struct {
	ID            uuid.UUID  `json:"id"`
	TaskID        uuid.UUID  `json:"taskId"`
	AuthorUserID  *uuid.UUID `json:"authorUserId"`
	AuthorTokenID *uuid.UUID `json:"authorTokenId"`
	AuthorKind    string     `json:"authorKind"`
	Body          string     `json:"body"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func fromRow(row db.TaskComment) TaskComment {
	return TaskComment{
		ID:            row.ID,
		TaskID:        row.TaskID,
		AuthorUserID:  uuidPtr(row.AuthorUserID),
		AuthorTokenID: uuidPtr(row.AuthorTokenID),
		AuthorKind:    row.AuthorKind,
		Body:          row.Body,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
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

// Service manages task comments inside projects owned by a single user, or
// scoped to a single project via a bearer token.
type Service struct {
	q        db.Querier
	txRunner database.TxRunner
	projects *project.Service
	tasks    *task.Service
}

// NewService constructs a taskcomment Service. projects and tasks are used
// to verify project role and task membership before any read or write.
// txRunner scopes a comment insert together with its outbound comment.create
// sync job enqueue (#104) into one commit, the same way internal/task does
// for a task write.
func NewService(q db.Querier, txRunner database.TxRunner, projects *project.Service, tasks *task.Service) *Service {
	return &Service{q: q, txRunner: txRunner, projects: projects, tasks: tasks}
}

// authorize requires ownerID to hold at least min on projectID, mapping
// project.ErrNotFound/ErrForbidden to this package's own sentinels, the same
// way task.Service.authorize and taskdependency.Service.authorize do.
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
		return fmt.Errorf("taskcomment: authorize: %w", err)
	}
}

// normalizeBody enforces the 1-10000 character rule; unlike a task title,
// leading/trailing whitespace is preserved rather than trimmed, since a
// comment's formatting (e.g. a pasted code block or log) may be meaningful.
func normalizeBody(raw string) (string, error) {
	if n := utf8.RuneCountInString(raw); n < 1 || n > maxBodyLength {
		return "", ErrInvalidBody
	}
	return raw, nil
}

// Create posts a comment as ownerID (a logged-in user), scoped through
// taskID's project like task.Service.Get. It returns ErrNotFound if taskID
// does not exist or belongs to another user's project the caller has no
// access to.
func (s *Service) Create(ctx context.Context, ownerID, taskID uuid.UUID, body string) (TaskComment, error) {
	t, err := s.tasks.Get(ctx, ownerID, taskID)
	if err != nil {
		return TaskComment{}, mapTaskErr(err)
	}
	if err := s.authorize(ctx, ownerID, t.ProjectID, project.RoleMember); err != nil {
		return TaskComment{}, err
	}
	return s.create(ctx, taskID, t.ProjectID, db.CreateTaskCommentParams{
		AuthorUserID: toUUID(&ownerID),
		AuthorKind:   AuthorKindUser,
	}, body)
}

// CreateForProject posts a comment as tokenID (a project API token) on
// behalf of projectID, the way task.Service.ContextForProject scopes a
// bearer-token caller. It returns ErrNotFound if taskID does not belong to
// projectID.
func (s *Service) CreateForProject(ctx context.Context, projectID, tokenID, taskID uuid.UUID, body string) (TaskComment, error) {
	if err := s.checkTaskInProject(ctx, projectID, taskID); err != nil {
		return TaskComment{}, err
	}
	return s.create(ctx, taskID, projectID, db.CreateTaskCommentParams{
		AuthorTokenID: toUUID(&tokenID),
		AuthorKind:    AuthorKindAgent,
	}, body)
}

// create validates body and inserts the comment, filling in params.TaskID
// and params.Body, then — inside the same transaction — enqueues a
// comment.create sync job if taskID has a GitLab link (#104), mirroring
// internal/task's enqueueIfLinked pattern. params must already carry exactly
// one of AuthorUserID/AuthorTokenID and the matching AuthorKind.
func (s *Service) create(ctx context.Context, taskID, projectID uuid.UUID, params db.CreateTaskCommentParams, body string) (TaskComment, error) {
	normalized, err := normalizeBody(body)
	if err != nil {
		return TaskComment{}, err
	}
	params.TaskID = taskID
	params.Body = normalized

	var result TaskComment
	err = s.txRunner.RunInTx(ctx, func(q db.Querier) error {
		row, err := q.CreateTaskComment(ctx, params)
		if err != nil {
			return fmt.Errorf("taskcomment: create: %w", err)
		}
		result = fromRow(row)
		return enqueueIfLinked(ctx, q, taskID, projectID, result.ID)
	})
	if err != nil {
		return TaskComment{}, err
	}
	return result, nil
}

// enqueueIfLinked enqueues a comment.create sync job for commentID only if
// taskID already has a GitLab link — a task with none is purely local, so
// there is nothing to push. Unlike issue.update's job, this carries no
// dedupe key: each comment is a distinct row, not an overwrite of a
// previous pending payload.
func enqueueIfLinked(ctx context.Context, q db.Querier, taskID, projectID, commentID uuid.UUID) error {
	if _, err := q.GetTaskGitlabLinkByTaskID(ctx, taskID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("taskcomment: check gitlab link: %w", err)
	}
	payload, err := json.Marshal(commentsync.CreatePayload{CommentID: commentID})
	if err != nil {
		return fmt.Errorf("taskcomment: encode sync payload: %w", err)
	}
	if _, err := syncpkg.Enqueue(ctx, q, syncpkg.EnqueueParams{
		ProjectID: projectID,
		TaskID:    &taskID,
		Kind:      commentsync.KindCommentCreate,
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("taskcomment: enqueue sync job: %w", err)
	}
	return nil
}

// List returns taskID's comments, oldest first, for a session-authenticated
// caller. It returns ErrNotFound if taskID does not exist or belongs to
// another user's project.
func (s *Service) List(ctx context.Context, ownerID, taskID uuid.UUID) ([]TaskComment, error) {
	t, err := s.tasks.Get(ctx, ownerID, taskID)
	if err != nil {
		return nil, mapTaskErr(err)
	}
	if err := s.authorize(ctx, ownerID, t.ProjectID, project.RoleViewer); err != nil {
		return nil, err
	}
	return s.list(ctx, taskID)
}

// ListForProject is List for a bearer-token caller already scoped to
// projectID by internal/http.requireTokenResourceProject. It returns
// ErrNotFound if taskID does not belong to projectID.
func (s *Service) ListForProject(ctx context.Context, projectID, taskID uuid.UUID) ([]TaskComment, error) {
	if err := s.checkTaskInProject(ctx, projectID, taskID); err != nil {
		return nil, err
	}
	return s.list(ctx, taskID)
}

func (s *Service) list(ctx context.Context, taskID uuid.UUID) ([]TaskComment, error) {
	rows, err := s.q.ListTaskCommentsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("taskcomment: list: %w", err)
	}
	out := make([]TaskComment, len(rows))
	for i, row := range rows {
		out[i] = fromRow(row)
	}
	return out, nil
}

// ListRecent returns taskID's most recent comments (at most RecentLimit),
// oldest first, for embedding into the AI-facing context response
// (task_context_handler.go). It performs no authorization of its own —
// callers must have already scoped taskID.
func ListRecent(all []TaskComment) []TaskComment {
	if len(all) <= RecentLimit {
		return all
	}
	return all[len(all)-RecentLimit:]
}

// Delete removes commentID, scoped through its task's project like Create,
// and only if it was authored by ownerID — deleting someone else's comment
// reports ErrForbidden. A 'gitlab'-authored comment has no AuthorUserID to
// match, so it can never be deleted this way (#104).
func (s *Service) Delete(ctx context.Context, ownerID, commentID uuid.UUID) error {
	comment, projectID, err := s.getForDelete(ctx, commentID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, ownerID, projectID, project.RoleMember); err != nil {
		return err
	}
	if comment.AuthorUserID == nil || *comment.AuthorUserID != ownerID {
		return ErrForbidden
	}
	return s.delete(ctx, commentID)
}

// DeleteForToken is Delete for a bearer-token caller already scoped to
// commentID's project by internal/http.requireTokenResourceProject, allowed
// only when commentID was authored by that same tokenID.
func (s *Service) DeleteForToken(ctx context.Context, tokenID, commentID uuid.UUID) error {
	comment, _, err := s.getForDelete(ctx, commentID)
	if err != nil {
		return err
	}
	if comment.AuthorTokenID == nil || *comment.AuthorTokenID != tokenID {
		return ErrForbidden
	}
	return s.delete(ctx, commentID)
}

func (s *Service) getForDelete(ctx context.Context, commentID uuid.UUID) (TaskComment, uuid.UUID, error) {
	row, err := s.q.GetTaskCommentByID(ctx, commentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TaskComment{}, uuid.Nil, ErrNotFound
		}
		return TaskComment{}, uuid.Nil, fmt.Errorf("taskcomment: get: %w", err)
	}
	projectID, err := s.q.GetTaskCommentProjectID(ctx, commentID)
	if err != nil {
		return TaskComment{}, uuid.Nil, fmt.Errorf("taskcomment: get project id: %w", err)
	}
	return fromRow(row), projectID, nil
}

func (s *Service) delete(ctx context.Context, commentID uuid.UUID) error {
	affected, err := s.q.DeleteTaskCommentByID(ctx, commentID)
	if err != nil {
		return fmt.Errorf("taskcomment: delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// checkTaskInProject verifies taskID belongs to projectID, for a
// bearer-token caller that has no owner to authorize against — the token's
// project match (already enforced by requireTokenResourceProject at the HTTP
// layer for commentID-shaped URLs) is authorization enough.
func (s *Service) checkTaskInProject(ctx context.Context, projectID, taskID uuid.UUID) error {
	actualProjectID, err := s.tasks.ProjectID(ctx, taskID)
	if err != nil {
		return mapTaskErr(err)
	}
	if actualProjectID != projectID {
		return ErrNotFound
	}
	return nil
}

// ProjectID returns the project commentID's task belongs to, with no
// authorization check — only requireTokenResourceProject
// (internal/http, issue #66) uses this, to compare against a bearer token's
// own project before DeleteForToken runs.
func (s *Service) ProjectID(ctx context.Context, commentID uuid.UUID) (uuid.UUID, error) {
	projectID, err := s.q.GetTaskCommentProjectID(ctx, commentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("taskcomment: project id: %w", err)
	}
	return projectID, nil
}

func mapTaskErr(err error) error {
	if errors.Is(err, task.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
