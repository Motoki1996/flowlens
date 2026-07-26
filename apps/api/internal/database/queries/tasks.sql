-- Tasks are scoped through their parent project the same way backlogs are:
-- every single-task query joins to projects and filters on
-- owner_user_id, so a foreign task is indistinguishable from a missing one.
-- CreateTask trusts the caller to have already verified project ownership
-- (e.g. via project.Service.Get), like CreateBacklog.
--
-- ListTasksByProject takes three independent optional filters so one query
-- serves the unfiltered, single-backlog, unassigned-only and status-scoped
-- list views: passing false/NULL/'' for a filter disables it.

-- name: CreateTask :one
INSERT INTO tasks (
    project_id, backlog_id, title, description,
    assignee_gitlab_user_id, assignee_gitlab_username,
    labels, due_on, created_by_user_id, position
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    COALESCE((SELECT MAX(position) + 1 FROM tasks WHERE project_id = $1 AND backlog_id IS NOT DISTINCT FROM $2), 0)
)
RETURNING id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at;

-- name: ListTasksByProject :many
SELECT id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at
FROM tasks
WHERE project_id = $1
  AND (NOT sqlc.arg(unassigned)::boolean OR backlog_id IS NULL)
  AND (sqlc.narg(backlog_id)::uuid IS NULL OR backlog_id = sqlc.narg(backlog_id))
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
ORDER BY position ASC, created_at ASC;

-- name: GetTaskForOwner :one
SELECT t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at
FROM tasks t
JOIN projects p ON p.id = t.project_id
WHERE t.id = $1 AND p.owner_user_id = $2;

-- name: UpdateTaskForOwner :one
UPDATE tasks t
SET backlog_id = $3, title = $4, description = $5,
    assignee_gitlab_user_id = $6, assignee_gitlab_username = $7,
    labels = $8, due_on = $9, position = $10, updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: AssignTaskBacklogForOwner :one
UPDATE tasks t
SET backlog_id = $3, updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: CloseTaskForOwner :one
UPDATE tasks t
SET status = 'closed', closed_at = now(), updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: ReopenTaskForOwner :one
UPDATE tasks t
SET status = 'open', closed_at = NULL, updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: DeleteTaskForOwner :execrows
DELETE FROM tasks t
USING projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2;
