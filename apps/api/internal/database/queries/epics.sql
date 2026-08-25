-- Epics (000032) are the optional rung between a backlog and its tasks.
-- Like backlogs they have no owner column of their own: ownership is always
-- checked through the parent project. CreateEpic/ListEpicsByProject trust the
-- caller to have already verified project access (e.g. via
-- project.Service.Authorize), while the single-epic queries join to
-- project_members so a foreign epic is indistinguishable from a missing one.
--
-- Every column here mirrors backlogs.sql's, deliberately — see the 000032
-- migration. backlog_id is the one addition, and it is nullable: an epic
-- outside any backlog is the Unclassified group, exactly as an unfiled task
-- is.

-- name: CreateEpic :one
INSERT INTO epics (project_id, backlog_id, name, description, position, start_date, due_on, priority, progress, default_linked_gitlab_project_id, base_branch, allowed_scope, forbidden_scope, assignee_user_id)
VALUES (
    $1,
    $2,
    $3,
    $4,
    COALESCE((SELECT MAX(position) + 1 FROM epics WHERE project_id = $1), 0),
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11,
    $12,
    $13
)
RETURNING *;

-- ListEpicsByProject follows ListBacklogsByProject exactly — the same
-- "empty/false disables it" filter convention, the same priority/progress
-- sort ranks, and the same LEFT JOIN task counts so the Epic collection
-- screen's List row count, Board card ratio and Timeline bar fill come from
-- one query. backlog_id is the extra filter: sqlc.narg(backlog_id) narrows to
-- one backlog's epics, and backlog_unfiled to the epics in no backlog at all.
-- name: ListEpicsByProject :many
SELECT
  e.id, e.project_id, e.backlog_id, e.name, e.description, e.position, e.created_at, e.updated_at,
  e.start_date, e.due_on, e.priority, e.progress, e.default_linked_gitlab_project_id, e.base_branch,
  e.allowed_scope, e.forbidden_scope, e.assignee_user_id,
  COUNT(t.id) AS task_count,
  COUNT(t.id) FILTER (WHERE t.status = 'closed') AS closed_task_count
FROM epics e
LEFT JOIN tasks t ON t.epic_id = e.id
WHERE e.project_id = $1
  AND (sqlc.narg(backlog_id)::uuid IS NULL OR e.backlog_id = sqlc.narg(backlog_id))
  AND (NOT sqlc.arg(backlog_unfiled)::boolean OR e.backlog_id IS NULL)
  AND (sqlc.arg(priority)::text = '' OR e.priority = sqlc.arg(priority))
  AND (sqlc.arg(progress)::text = '' OR e.progress = sqlc.arg(progress))
  AND (sqlc.narg(assignee_user_id)::uuid IS NULL OR e.assignee_user_id = sqlc.narg(assignee_user_id))
  AND (NOT sqlc.arg(assignee_unassigned)::boolean OR e.assignee_user_id IS NULL)
GROUP BY e.id
ORDER BY
  (CASE WHEN sqlc.arg(sort_by_priority)::boolean THEN
     CASE e.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END
   ELSE 0 END) DESC,
  (CASE WHEN sqlc.arg(sort_by_progress)::boolean THEN
     CASE e.progress WHEN 'not_started' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'on_hold' THEN 3 WHEN 'done' THEN 4 ELSE 0 END
   ELSE 0 END) ASC,
  e.position ASC, e.created_at ASC;

-- name: GetEpicForOwner :one
SELECT e.*
FROM epics e
WHERE e.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = e.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- GetEpicProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http) uses to enforce a bearer
-- token's project boundary on a single-epic URL, without GetEpicForOwner's
-- member join — a token has no session owner to join against.
-- name: GetEpicProjectID :one
SELECT project_id FROM epics WHERE id = $1;

-- GetEpicTaskDefaults is GetBacklogTaskDefaults' epic-rung twin: the
-- lightweight lookup internal/task's Context uses to resolve a task's epic's
-- base_branch/allowed_scope/forbidden_scope. The two are resolved per field
-- (epic value if non-empty, else the backlog's), not per object, so an epic
-- that sets only base_branch still inherits its backlog's scope.
-- name: GetEpicTaskDefaults :one
SELECT base_branch, allowed_scope, forbidden_scope, backlog_id FROM epics WHERE id = $1;

-- GetEpicIssueDestination is the create-time lookup internal/task uses to
-- resolve where a new task's issue is filed: the epic's own link, and the
-- backlog it belongs to so the chain can fall through to that backlog's link
-- and then the project default.
-- name: GetEpicIssueDestination :one
SELECT default_linked_gitlab_project_id, backlog_id FROM epics WHERE id = $1;

-- ReorderEpics resequences a project's epics to epic_ids' given order
-- (position 0 for the first id, 1 for the second, ...) in a single
-- statement, the same all-or-nothing shape as ReorderBacklogs.
-- internal/epic.Service.Reorder checks epic_ids is exactly the project's
-- current epic set before calling this.
-- name: ReorderEpics :exec
WITH ordered AS (
    SELECT id, (ord - 1)::int AS position
    FROM unnest(sqlc.arg(epic_ids)::uuid[]) WITH ORDINALITY AS t(id, ord)
)
UPDATE epics
SET position = ordered.position, updated_at = now()
FROM ordered
WHERE epics.id = ordered.id
  AND epics.project_id = sqlc.arg(project_id);

-- UpdateEpicForOwner overwrites every editable column, so the optional ones
-- must arrive already resolved: epic.Service reads the current row first and
-- fills in whatever the PATCH body left out (see its Update).
-- name: UpdateEpicForOwner :one
UPDATE epics e
SET backlog_id = $2, name = $3, description = $4, position = $5, start_date = $6, due_on = $7, priority = $8, progress = $9, default_linked_gitlab_project_id = $10, base_branch = $11, allowed_scope = $12, forbidden_scope = $13,
    assignee_user_id = $14, updated_at = now()
WHERE e.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = e.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING e.*;

-- MoveEpicTasksToBacklog keeps a task's backlog_id in step with its epic's
-- when the epic itself moves between backlogs (internal/epic.Service.Update
-- runs it in the same transaction as UpdateEpicForOwner). Without it a task
-- would report a backlog its own epic no longer belongs to.
-- name: MoveEpicTasksToBacklog :exec
UPDATE tasks
SET backlog_id = sqlc.narg(backlog_id), updated_at = now()
WHERE epic_id = sqlc.arg(epic_id)
  AND backlog_id IS DISTINCT FROM sqlc.narg(backlog_id);

-- name: DeleteEpicForOwner :execrows
DELETE FROM epics e
WHERE e.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = e.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  );

-- SetEpicTasks is the declarative half of "which tasks are in this epic": the
-- caller sends the whole set, and these two statements make the table match
-- it inside one transaction (internal/epic.Service.SetTasks). A per-task
-- PATCH loop could leave a half-applied epic behind if one call failed;
-- moving several tasks at once is exactly the operation that must not.

-- ClearEpicTasksExcept unfiles every task currently in the epic that the new
-- set no longer names. The tasks keep their backlog — dropping out of an epic
-- returns a task to sitting directly in it, the same as deleting the epic.
-- name: ClearEpicTasksExcept :exec
UPDATE tasks
SET epic_id = NULL, updated_at = now()
WHERE epic_id = sqlc.arg(epic_id)
  AND NOT (id = ANY(sqlc.arg(task_ids)::uuid[]));

-- AssignTasksToEpic files the named tasks under the epic, writing the epic's
-- own backlog onto each in the same statement — a task's epic and backlog
-- must agree (000032), and this is the one write that can move a task into an
-- epic without going through internal/task.
--
-- The project_id check is what stops an epic from adopting another project's
-- task; the row count tells the caller whether every id actually matched, so
-- a foreign or missing id rolls the whole set back rather than silently
-- moving the rest.
-- name: AssignTasksToEpic :execrows
UPDATE tasks t
SET epic_id = e.id, backlog_id = e.backlog_id, updated_at = now()
FROM epics e
WHERE e.id = sqlc.arg(epic_id)
  AND t.id = ANY(sqlc.arg(task_ids)::uuid[])
  AND t.project_id = e.project_id;

-- CountTasksInProjectByIDs is SetEpicTasks' pre-check: how many of the given
-- ids are really tasks in the epic's project. internal/epic rejects the whole
-- request before writing anything when the count falls short, so a foreign or
-- missing id is refused rather than rolled back — the guarantee then holds
-- independently of transaction semantics, which is also what lets the
-- in-memory fake (dbtest) reproduce it.
-- name: CountTasksInProjectByIDs :one
SELECT COUNT(*) FROM tasks
WHERE project_id = sqlc.arg(project_id)
  AND id = ANY(sqlc.arg(task_ids)::uuid[]);
