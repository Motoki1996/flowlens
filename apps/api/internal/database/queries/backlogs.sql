-- Backlogs have no owner column of their own; ownership is always checked
-- through the parent project. CreateBacklog/ListBacklogsByProject trust the
-- caller to have already verified project ownership (e.g. via
-- project.Service.Get), while the single-backlog queries join to projects so
-- a foreign backlog is indistinguishable from a missing one.

-- name: CreateBacklog :one
INSERT INTO backlogs (project_id, name, description, position, start_date, due_on, priority, progress)
VALUES (
    $1,
    $2,
    $3,
    COALESCE((SELECT MAX(position) + 1 FROM backlogs WHERE project_id = $1), 0),
    $4,
    $5,
    $6,
    $7
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
-- name: ListBacklogsByProject :many
SELECT * FROM backlogs
WHERE project_id = $1
  AND (sqlc.arg(priority)::text = '' OR priority = sqlc.arg(priority))
  AND (sqlc.arg(progress)::text = '' OR progress = sqlc.arg(progress))
ORDER BY
  (CASE WHEN sqlc.arg(sort_by_priority)::boolean THEN
     CASE priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END
   ELSE 0 END) DESC,
  (CASE WHEN sqlc.arg(sort_by_progress)::boolean THEN
     CASE progress WHEN 'not_started' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'on_hold' THEN 3 WHEN 'done' THEN 4 ELSE 0 END
   ELSE 0 END) ASC,
  position ASC, created_at ASC;

-- name: GetBacklogForOwner :one
SELECT b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at, b.start_date, b.due_on, b.priority, b.progress
FROM backlogs b
JOIN projects p ON p.id = b.project_id
WHERE b.id = $1 AND p.owner_user_id = $2;

-- GetBacklogProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-backlog URL, without
-- GetBacklogForOwner's owner join — a token has no session owner to join
-- against.

-- name: GetBacklogProjectID :one
SELECT project_id FROM backlogs WHERE id = $1;

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

-- UpdateBacklogForOwner overwrites every editable column, so start_date/due_on
-- must arrive already resolved: backlog.Service reads the current row first and
-- fills in whatever the PATCH body left out (see its Update).
-- name: UpdateBacklogForOwner :one
UPDATE backlogs b
SET name = $3, description = $4, position = $5, start_date = $6, due_on = $7, priority = $8, progress = $9, updated_at = now()
FROM projects p
WHERE b.id = $1 AND b.project_id = p.id AND p.owner_user_id = $2
RETURNING b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at, b.start_date, b.due_on, b.priority, b.progress;

-- name: DeleteBacklogForOwner :execrows
DELETE FROM backlogs b
USING projects p
WHERE b.id = $1 AND b.project_id = p.id AND p.owner_user_id = $2;
