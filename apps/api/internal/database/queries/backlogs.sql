-- Backlogs have no owner column of their own; ownership is always checked
-- through the parent project. CreateBacklog/ListBacklogsByProject trust the
-- caller to have already verified project ownership (e.g. via
-- project.Service.Get), while the single-backlog queries join to projects so
-- a foreign backlog is indistinguishable from a missing one.

-- default_linked_gitlab_project_id is the backlog's own destination for new
-- issues, overriding the project's default link (000021). NULL means "use the
-- project default". internal/backlog checks the link belongs to this project's
-- GitLab connection before writing it — the schema cannot.
--
-- base_branch (000024) is the branch tasks in this backlog are meant to
-- branch from; app-only, never synced to GitLab. allowed_scope/
-- forbidden_scope (000029) are the same kind of field: the paths tasks
-- filed in this backlog may/may not touch.
-- name: CreateBacklog :one
INSERT INTO backlogs (project_id, name, description, position, start_date, due_on, priority, progress, default_linked_gitlab_project_id, base_branch, allowed_scope, forbidden_scope)
VALUES (
    $1,
    $2,
    $3,
    COALESCE((SELECT MAX(position) + 1 FROM backlogs WHERE project_id = $1), 0),
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11
)
RETURNING *;

-- ListBacklogsByProject's priority and progress filters and sorts follow the
-- same "empty/false disables it" convention as internal/task's
-- ListTasksByProject. Sorting by priority ranks urgent > high > medium > low;
-- sorting by progress runs the other way, not_started first through done, so
-- the order reads as the work advancing (and matches the Board view's
-- left-to-right axis). Both fall back to the usual position/created_at order
-- as a tiebreak. sort_by_priority and sort_by_progress are mutually exclusive
-- in practice — internal/backlog sets at most one from a single ?sort=.
--
-- The LEFT JOIN to tasks (issue #144) computes each backlog's task_count and
-- closed_task_count in the same query, so the Backlog collection screen (its
-- List row count, Board card ratio and Timeline bar fill) doesn't need to
-- fetch every task in the project just to derive them.
-- name: ListBacklogsByProject :many
SELECT
  b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at,
  b.start_date, b.due_on, b.priority, b.progress, b.default_linked_gitlab_project_id, b.base_branch,
  b.allowed_scope, b.forbidden_scope,
  COUNT(t.id) AS task_count,
  COUNT(t.id) FILTER (WHERE t.status = 'closed') AS closed_task_count
FROM backlogs b
LEFT JOIN tasks t ON t.backlog_id = b.id
WHERE b.project_id = $1
  AND (sqlc.arg(priority)::text = '' OR b.priority = sqlc.arg(priority))
  AND (sqlc.arg(progress)::text = '' OR b.progress = sqlc.arg(progress))
GROUP BY b.id
ORDER BY
  (CASE WHEN sqlc.arg(sort_by_priority)::boolean THEN
     CASE b.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END
   ELSE 0 END) DESC,
  (CASE WHEN sqlc.arg(sort_by_progress)::boolean THEN
     CASE b.progress WHEN 'not_started' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'on_hold' THEN 3 WHEN 'done' THEN 4 ELSE 0 END
   ELSE 0 END) ASC,
  b.position ASC, b.created_at ASC;

-- name: GetBacklogForOwner :one
SELECT b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at, b.start_date, b.due_on, b.priority, b.progress, b.default_linked_gitlab_project_id, b.base_branch, b.allowed_scope, b.forbidden_scope
FROM backlogs b
WHERE b.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = b.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- GetBacklogProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-backlog URL, without
-- GetBacklogForOwner's owner join — a token has no session owner to join
-- against.

-- name: GetBacklogProjectID :one
SELECT project_id FROM backlogs WHERE id = $1;

-- GetBacklogTaskDefaults is the lightweight lookup internal/task's Context
-- uses to resolve a task's backlog's base_branch/allowed_scope/
-- forbidden_scope without pulling in the rest of the backlog row.
-- name: GetBacklogTaskDefaults :one
SELECT base_branch, allowed_scope, forbidden_scope FROM backlogs WHERE id = $1;

-- ReorderBacklogs resequences a project's backlogs to backlog_ids' given
-- order (position 0 for the first id, 1 for the second, ...) in a single
-- statement, the same all-or-nothing shape as internal/task's ReorderTasks
-- (issue #79). internal/backlog.Service.Reorder checks backlog_ids is
-- exactly the project's current backlog set before calling this.

-- name: ReorderBacklogs :exec
WITH ordered AS (
    SELECT id, (ord - 1)::int AS position
    FROM unnest(sqlc.arg(backlog_ids)::uuid[]) WITH ORDINALITY AS t(id, ord)
)
UPDATE backlogs
SET position = ordered.position, updated_at = now()
FROM ordered
WHERE backlogs.id = ordered.id
  AND backlogs.project_id = sqlc.arg(project_id);

-- UpdateBacklogForOwner overwrites every editable column, so start_date/due_on,
-- default_linked_gitlab_project_id, base_branch, allowed_scope and
-- forbidden_scope must arrive already resolved: backlog.Service reads the
-- current row first and fills in whatever the PATCH body left out (see its
-- Update).
-- name: UpdateBacklogForOwner :one
UPDATE backlogs b
SET name = $2, description = $3, position = $4, start_date = $5, due_on = $6, priority = $7, progress = $8, default_linked_gitlab_project_id = $9, base_branch = $10, allowed_scope = $11, forbidden_scope = $12, updated_at = now()
WHERE b.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = b.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at, b.start_date, b.due_on, b.priority, b.progress, b.default_linked_gitlab_project_id, b.base_branch, b.allowed_scope, b.forbidden_scope;

-- name: DeleteBacklogForOwner :execrows
DELETE FROM backlogs b
WHERE b.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = b.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  );
