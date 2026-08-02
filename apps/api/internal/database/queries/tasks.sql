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
    labels, due_on, start_date, created_by_user_id, position
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    COALESCE((SELECT MAX(position) + 1 FROM tasks WHERE project_id = $1 AND backlog_id IS NOT DISTINCT FROM $2), 0)
)
RETURNING id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, start_date, position, created_by_user_id, created_at, updated_at;

-- name: ListTasksByProject :many
SELECT id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, start_date, position, created_by_user_id, created_at, updated_at
FROM tasks
WHERE project_id = $1
  AND (NOT sqlc.arg(unassigned)::boolean OR backlog_id IS NULL)
  AND (sqlc.narg(backlog_id)::uuid IS NULL OR backlog_id = sqlc.narg(backlog_id))
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
ORDER BY position ASC, created_at ASC;

-- ListTasksByProjectPaged backs the AI-facing bulk context endpoint (GET
-- /api/v1/projects/{projectID}/tasks/context, docs/plans/issue-sync.md
-- "AI-facing"): the same backlog_id/status filters as ListTasksByProject
-- plus updated_since (tasks.updated_at >= the given time) and LIMIT/OFFSET
-- paging, following the "fetch one extra row to detect a next page"
-- convention ListWebhookEventsByLinkedGitlabProjectID/internal/webhookevent
-- established. It has no "unassigned" filter — the bulk context view has no
-- use for it yet, unlike the board view ListTasksByProject serves.

-- name: ListTasksByProjectPaged :many
SELECT id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, start_date, position, created_by_user_id, created_at, updated_at
FROM tasks
WHERE project_id = sqlc.arg(project_id)
  AND (sqlc.narg(backlog_id)::uuid IS NULL OR backlog_id = sqlc.narg(backlog_id))
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
  AND (sqlc.narg(updated_since)::timestamptz IS NULL OR updated_at >= sqlc.narg(updated_since))
ORDER BY position ASC, created_at ASC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: GetTaskForOwner :one
SELECT t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.start_date, t.position, t.created_by_user_id, t.created_at, t.updated_at
FROM tasks t
JOIN projects p ON p.id = t.project_id
WHERE t.id = $1 AND p.owner_user_id = $2;

-- GetTaskForProject scopes a task by its project directly, not by owner: the
-- AI-facing bearer-token path (docs/plans/issue-sync.md "AI-facing") has no
-- session user, only the project a project.Service-verified token was issued
-- for (internal/apitoken), so there is no owner to join against.

-- name: GetTaskForProject :one
SELECT id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, start_date, position, created_by_user_id, created_at, updated_at
FROM tasks
WHERE id = $1 AND project_id = $2;

-- GetTaskProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-task URL. It has no owner
-- join, unlike GetTaskForOwner: a token has no session owner to join
-- against (apitoken.Auth), only its own project to compare the result
-- against.

-- name: GetTaskProjectID :one
SELECT project_id FROM tasks WHERE id = $1;

-- name: UpdateTaskForOwner :one
UPDATE tasks t
SET backlog_id = $3, title = $4, description = $5,
    assignee_gitlab_user_id = $6, assignee_gitlab_username = $7,
    labels = $8, due_on = $9, start_date = $10, position = $11, updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.start_date, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: AssignTaskBacklogForOwner :one
UPDATE tasks t
SET backlog_id = $3, updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.start_date, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: CloseTaskForOwner :one
UPDATE tasks t
SET status = 'closed', closed_at = now(), updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.start_date, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: ReopenTaskForOwner :one
UPDATE tasks t
SET status = 'open', closed_at = NULL, updated_at = now()
FROM projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.start_date, t.position, t.created_by_user_id, t.created_at, t.updated_at;

-- name: DeleteTaskForOwner :execrows
DELETE FROM tasks t
USING projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2;

-- CountFailedSyncTasksByProjectForOwner backs the project single view's sync
-- warning (docs/plans/issue-sync.md's "gitlab" fields, surfaced per-task by
-- internal/task). A task counts as failed the same way internal/task derives
-- a single task's sync_status: from task_gitlab_links when a link exists, or
-- from its most recent sync_jobs row (necessarily an issue.create, see
-- GetLatestSyncJobForTask) when it doesn't.

-- ApplyWebhookTaskFields is the inbound apply pipeline's write to tasks
-- (internal/webhookapply, docs/plans/issue-sync.md "Inbound"): unscoped, like
-- GetLinkedGitlabProjectByID, because the task_gitlab_links row that names
-- this task ID was already authorized when the link was first created (or,
-- for a brand-new unclassified task, in the same transaction that just
-- created it). It never touches backlog_id or position, which are app-only.
-- closed_at only advances when status transitions into 'closed' — the CASE
-- reads the pre-update closed_at (every SET expression in one UPDATE sees
-- the same original row), so re-applying while already closed never moves
-- the timestamp, mirroring internal/task.Service.Close's own no-op rule.

-- name: ApplyWebhookTaskFields :one
UPDATE tasks
SET title = $2,
    description = $3,
    assignee_gitlab_user_id = $4,
    assignee_gitlab_username = $5,
    labels = $6,
    due_on = $7,
    status = $8,
    closed_at = CASE WHEN $8 = 'closed' THEN COALESCE(closed_at, now()) ELSE NULL END,
    updated_at = now()
WHERE id = $1
RETURNING id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, start_date, position, created_by_user_id, created_at, updated_at;

-- name: CountFailedSyncTasksByProjectForOwner :one
SELECT COUNT(*) FROM tasks t
JOIN projects p ON p.id = t.project_id
LEFT JOIN task_gitlab_links tgl ON tgl.task_id = t.id
WHERE t.project_id = $1 AND p.owner_user_id = $2
  AND (
    tgl.sync_status = 'failed'
    OR (tgl.task_id IS NULL AND EXISTS (
      SELECT 1 FROM sync_jobs sj WHERE sj.task_id = t.id AND sj.status = 'failed'
    ))
  );
