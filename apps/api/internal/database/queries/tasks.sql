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
    labels, due_on, start_date, priority, progress, size, created_by_user_id, assignee_user_id, position
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
    COALESCE((SELECT MAX(position) + 1 FROM tasks WHERE project_id = $1 AND backlog_id IS NOT DISTINCT FROM $2), 0)
)
RETURNING id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at, start_date, priority, progress, search_vector, design_started_at, implementation_started_at, size, assignee_user_id;

-- ListTasksByProject's priority, progress and size filters and sorts follow the same
-- "empty/false disables it" convention as the other three filters. Sorting by
-- priority ranks urgent > high > medium > low; sorting by progress runs the
-- other way, not_started first through done, so the order reads as the work
-- advancing (and matches the Board view's left-to-right axis). Sorting by size
-- ranks xl > l > m > s > xs, biggest first, the same direction priority runs.
-- All three fall back to the usual position/created_at order as a tiebreak,
-- so equal-ranked tasks keep their manual order instead of shuffling; when a
-- sort_by_* flag is false its CASE always evaluates to 0, leaving the
-- original position/created_at order untouched. The three flags are mutually
-- exclusive in practice — internal/task sets at most one from a single
-- ?sort=.
-- ListTasksByProject's assignee_user_id filter matches a task assigned to
-- that user on *either* axis: tasks.assignee_user_id (the FlowLens assignee,
-- 000031) or, through the project's GitLab connection joined to that same
-- user's registered identity for that base_url (internal/gitlabidentity,
-- issue #102), tasks.assignee_gitlab_user_id. The two are ORed because a
-- task synced in from GitLab carries only the latter and a purely local task
-- only the former, and a caller filtering by a person means the person, not
-- one of their two identities. The identity join is keyed on the *filter
-- target*, not the caller, which is what makes ?assignee=<someone else>
-- behave exactly like ?assignee=me. A target with no registered identity, or
-- a project with no GitLab connection, joins to NULL, and NULL never equals
-- assignee_gitlab_user_id, so that half of the OR simply never matches
-- instead of erroring. assignee_unassigned is the complement: assigned to
-- nobody on either axis.
-- ListTasksByProject's q filter (issue #106) matches tasks.search_vector
-- (the 'simple'-config tsvector generated column, see the 000016
-- migration) against websearch_to_tsquery, GIN-indexed; an empty q disables
-- it the same way every other filter here does.
-- name: ListTasksByProject :many
SELECT tasks.id, tasks.project_id, tasks.backlog_id, tasks.title, tasks.description, tasks.status, tasks.closed_at, tasks.assignee_gitlab_user_id, tasks.assignee_gitlab_username, tasks.labels, tasks.due_on, tasks.position, tasks.created_by_user_id, tasks.created_at, tasks.updated_at, tasks.start_date, tasks.priority, tasks.progress, tasks.search_vector, tasks.design_started_at, tasks.implementation_started_at, tasks.size, tasks.assignee_user_id
FROM tasks
LEFT JOIN gitlab_connections gc ON gc.project_id = tasks.project_id
LEFT JOIN user_gitlab_identities ugi ON ugi.gitlab_base_url = gc.base_url AND ugi.user_id = sqlc.narg(assignee_user_id)
WHERE tasks.project_id = sqlc.arg(project_id)
  AND (NOT sqlc.arg(unassigned)::boolean OR tasks.backlog_id IS NULL)
  AND (sqlc.narg(backlog_id)::uuid IS NULL OR tasks.backlog_id = sqlc.narg(backlog_id))
  AND (sqlc.arg(status)::text = '' OR tasks.status = sqlc.arg(status))
  AND (sqlc.arg(priority)::text = '' OR tasks.priority = sqlc.arg(priority))
  AND (sqlc.arg(progress)::text = '' OR tasks.progress = sqlc.arg(progress))
  AND (sqlc.arg(size)::text = '' OR tasks.size = sqlc.arg(size))
  AND (sqlc.narg(assignee_user_id)::uuid IS NULL
       OR tasks.assignee_user_id = sqlc.narg(assignee_user_id)
       OR tasks.assignee_gitlab_user_id = ugi.gitlab_user_id)
  AND (NOT sqlc.arg(assignee_unassigned)::boolean
       OR (tasks.assignee_user_id IS NULL AND tasks.assignee_gitlab_user_id IS NULL))
  AND (sqlc.arg(q)::text = '' OR tasks.search_vector @@ websearch_to_tsquery('simple', sqlc.arg(q)::text))
ORDER BY
  (CASE WHEN sqlc.arg(sort_by_priority)::boolean THEN
     CASE tasks.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END
   ELSE 0 END) DESC,
  (CASE WHEN sqlc.arg(sort_by_progress)::boolean THEN
     CASE tasks.progress WHEN 'not_started' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'on_hold' THEN 3 WHEN 'done' THEN 4 ELSE 0 END
   ELSE 0 END) ASC,
  (CASE WHEN sqlc.arg(sort_by_size)::boolean THEN
     CASE tasks.size WHEN 'xl' THEN 5 WHEN 'l' THEN 4 WHEN 'm' THEN 3 WHEN 's' THEN 2 WHEN 'xs' THEN 1 ELSE 0 END
   ELSE 0 END) DESC,
  tasks.position ASC, tasks.created_at ASC;

-- ListTasksByProjectPaged backs the AI-facing bulk context endpoint (GET
-- /api/v1/projects/{projectID}/tasks/context, docs/plans/issue-sync.md
-- "AI-facing"): the same backlog_id/status filters as ListTasksByProject
-- plus updated_since (tasks.updated_at >= the given time) and LIMIT/OFFSET
-- paging, following the "fetch one extra row to detect a next page"
-- convention ListWebhookEventsByLinkedGitlabProjectID/internal/webhookevent
-- established. It has no "unassigned" filter — the bulk context view has no
-- use for it yet, unlike the board view ListTasksByProject serves.

-- name: ListTasksByProjectPaged :many
SELECT id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at, start_date, priority, progress, search_vector, design_started_at, implementation_started_at, size, assignee_user_id
FROM tasks
WHERE project_id = sqlc.arg(project_id)
  AND (sqlc.narg(backlog_id)::uuid IS NULL OR backlog_id = sqlc.narg(backlog_id))
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
  AND (sqlc.narg(updated_since)::timestamptz IS NULL OR updated_at >= sqlc.narg(updated_since))
ORDER BY position ASC, created_at ASC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- ListTasksForOwner backs the cross-project task collection (GET
-- /api/v1/tasks, issue #76): every task across every project ownerID owns,
-- narrowed by the same status/priority/progress/size filters as ListTasksByProject plus
-- due/start date ranges and an optional project_id allowlist — each
-- following the same "empty/NULL disables it" convention. project_name is
-- joined in for the same reason GetTaskGitlabLinkWithProjectPathByTaskID
-- joins in path_with_namespace: a cross-project row is unreadable without
-- knowing which project it belongs to, and a second per-row fetch would
-- cost the same round trip anyway.
--
-- The ORDER BY leans on two properties: sqlc.arg(sort) is a single bound
-- value for the whole query, not a per-row one, so a CASE guarded by it is
-- either non-NULL for every row or NULL for every row — never a mix: sort
-- values not being ranked stay a true tie and fall through instead of
-- corrupting the row order. And due_on ASC's default NULLS LAST (Postgres
-- sorts NULL last on ASC, first on DESC) is exactly "tasks with no due date
-- sink to the bottom", so it works unguarded both as sort=dueOn's primary
-- key and as every other sort's final tiebreak.

-- ListTasksForMember's assignee_user_id/assignee_unassigned filters follow
-- the same two-axis rule ListTasksByProject's do, with the
-- gitlab_connections/user_gitlab_identities join resolved per-project since
-- the cross-project list spans however many projects and GitLab connections
-- the caller belongs to. Note owner_user_id still scopes *membership* here
-- (pm.user_id) and is independent of who is being filtered for.
-- ListTasksForMember's q filter is the same search_vector match
-- ListTasksByProject's is (issue #106).
-- name: ListTasksForMember :many
SELECT t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.size, t.assignee_user_id,
       p.name AS project_name
FROM tasks t
JOIN projects p ON p.id = t.project_id
JOIN project_members pm ON pm.project_id = p.id
LEFT JOIN gitlab_connections gc ON gc.project_id = p.id
LEFT JOIN user_gitlab_identities ugi ON ugi.gitlab_base_url = gc.base_url AND ugi.user_id = sqlc.narg(assignee_user_id)
WHERE pm.user_id = sqlc.arg(owner_user_id)
  AND (sqlc.arg(status)::text = '' OR t.status = sqlc.arg(status))
  AND (sqlc.arg(priority)::text = '' OR t.priority = sqlc.arg(priority))
  AND (sqlc.arg(progress)::text = '' OR t.progress = sqlc.arg(progress))
  AND (sqlc.arg(size)::text = '' OR t.size = sqlc.arg(size))
  AND (sqlc.narg(due_before)::date IS NULL OR t.due_on <= sqlc.narg(due_before))
  AND (sqlc.narg(due_after)::date IS NULL OR t.due_on >= sqlc.narg(due_after))
  AND (sqlc.narg(started_before)::date IS NULL OR t.start_date <= sqlc.narg(started_before))
  AND (cardinality(sqlc.arg(project_ids)::uuid[]) = 0 OR t.project_id = ANY(sqlc.arg(project_ids)::uuid[]))
  AND (sqlc.narg(assignee_user_id)::uuid IS NULL
       OR t.assignee_user_id = sqlc.narg(assignee_user_id)
       OR t.assignee_gitlab_user_id = ugi.gitlab_user_id)
  AND (NOT sqlc.arg(assignee_unassigned)::boolean
       OR (t.assignee_user_id IS NULL AND t.assignee_gitlab_user_id IS NULL))
  AND (sqlc.arg(q)::text = '' OR t.search_vector @@ websearch_to_tsquery('simple', sqlc.arg(q)::text))
ORDER BY
  (CASE WHEN sqlc.arg(sort)::text = 'priority' THEN
     CASE t.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END
   END) DESC,
  (CASE WHEN sqlc.arg(sort)::text = 'progress' THEN
     CASE t.progress WHEN 'not_started' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'on_hold' THEN 3 WHEN 'done' THEN 4 ELSE 0 END
   END) ASC,
  (CASE WHEN sqlc.arg(sort)::text = 'size' THEN
     CASE t.size WHEN 'xl' THEN 5 WHEN 'l' THEN 4 WHEN 'm' THEN 3 WHEN 's' THEN 2 WHEN 'xs' THEN 1 ELSE 0 END
   END) DESC,
  (CASE WHEN sqlc.arg(sort)::text = 'updatedAt' THEN t.updated_at END) DESC,
  t.due_on ASC,
  t.position ASC, t.created_at ASC
LIMIT sqlc.arg(limit_count);

-- name: GetTaskForOwner :one
SELECT t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id
FROM tasks t
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- GetTaskForProject scopes a task by its project directly, not by owner: the
-- AI-facing bearer-token path (docs/plans/issue-sync.md "AI-facing") has no
-- session user, only the project a project.Service-verified token was issued
-- for (internal/apitoken), so there is no owner to join against.

-- name: GetTaskForProject :one
SELECT id, project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at, start_date, priority, progress, search_vector, design_started_at, implementation_started_at, size, assignee_user_id
FROM tasks
WHERE tasks.id = $1 AND tasks.project_id = $2;

-- GetTaskProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-task URL. It has no owner
-- join, unlike GetTaskForOwner: a token has no session owner to join
-- against (apitoken.Auth), only its own project to compare the result
-- against.

-- name: GetTaskProjectID :one
SELECT project_id FROM tasks WHERE id = $1;

-- ReorderTasks resequences one backlog bucket's tasks to task_ids' given
-- order (position 0 for the first id, 1 for the second, ...) in a single
-- statement, so a drag across many tasks either lands as one committed order
-- or fails outright, never a partially-applied one (issue #79). backlog_id
-- follows CreateTask's own "IS NOT DISTINCT FROM" convention: NULL scopes to
-- the Unclassified bucket, a UUID to one specific backlog.
-- internal/task.Service.Reorder checks task_ids is exactly that bucket's
-- current task set before calling this, so the WHERE clause can never miss a
-- row or touch one outside the intended bucket.

-- name: ReorderTasks :exec
WITH ordered AS (
    SELECT id, (ord - 1)::int AS position
    FROM unnest(sqlc.arg(task_ids)::uuid[]) WITH ORDINALITY AS t(id, ord)
)
UPDATE tasks
SET position = ordered.position, updated_at = now()
FROM ordered
WHERE tasks.id = ordered.id
  AND tasks.project_id = sqlc.arg(project_id)
  AND tasks.backlog_id IS NOT DISTINCT FROM sqlc.narg(backlog_id);

-- name: UpdateTaskForOwner :one
UPDATE tasks t
SET backlog_id = $2, title = $3, description = $4,
    assignee_gitlab_user_id = $5, assignee_gitlab_username = $6,
    labels = $7, due_on = $8, start_date = $9, priority = $10, progress = $11, size = $12, position = $13,
    assignee_user_id = $14, updated_at = now()
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id;

-- name: AssignTaskBacklogForOwner :one
UPDATE tasks t
SET backlog_id = $2, updated_at = now()
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id;

-- name: CloseTaskForOwner :one
UPDATE tasks t
SET status = 'closed', closed_at = now(), updated_at = now()
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id;

-- name: ReopenTaskForOwner :one
UPDATE tasks t
SET status = 'open', closed_at = NULL, updated_at = now()
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id;

-- name: DeleteTaskForOwner :execrows
DELETE FROM tasks t
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  );

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
-- created it). It never touches backlog_id, position, priority, progress or
-- size, which are app-only — in particular an issue being closed on GitLab moves
-- status, never progress (see the 000011 migration).
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
WHERE tasks.id = $1
RETURNING tasks.id, tasks.project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at, start_date, priority, progress, search_vector, design_started_at, implementation_started_at, size, assignee_user_id;

-- ApplyGitlabProgressDone is internal/progresssync's write (issue #202):
-- called only from the same transaction as an ApplyWebhookTaskFields
-- status='closed' write, and only when progress_sync_settings.enabled is
-- true for the task's project and progress isn't already 'done'. Unscoped
-- like ApplyWebhookTaskFields, for the same reason.

-- name: ApplyGitlabProgressDone :one
UPDATE tasks
SET progress = 'done',
    updated_at = now()
WHERE tasks.id = $1
RETURNING tasks.id, tasks.project_id, backlog_id, title, description, status, closed_at, assignee_gitlab_user_id, assignee_gitlab_username, labels, due_on, position, created_by_user_id, created_at, updated_at, start_date, priority, progress, search_vector, design_started_at, implementation_started_at, size, assignee_user_id;

-- MarkTaskDesignStarted and MarkTaskImplementationStarted back the two
-- spec-driven-development phase-marker endpoints (POST
-- .../design-started, .../implementation-started): unlike every other task
-- field, they always overwrite, no "already set" guard — calling the
-- endpoint again (e.g. redoing the design) just moves the timestamp
-- forward.

-- name: MarkTaskDesignStarted :one
UPDATE tasks t
SET design_started_at = now(), updated_at = now()
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id;

-- name: MarkTaskImplementationStarted :one
UPDATE tasks t
SET implementation_started_at = now(), updated_at = now()
WHERE t.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING t.id, t.project_id, t.backlog_id, t.title, t.description, t.status, t.closed_at, t.assignee_gitlab_user_id, t.assignee_gitlab_username, t.labels, t.due_on, t.position, t.created_by_user_id, t.created_at, t.updated_at, t.start_date, t.priority, t.progress, t.search_vector, t.design_started_at, t.implementation_started_at, t.size, t.assignee_user_id;

-- name: CountFailedSyncTasksByProjectForOwner :one
SELECT COUNT(*) FROM tasks t
LEFT JOIN task_gitlab_links tgl ON tgl.task_id = t.id
WHERE t.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND (
    tgl.sync_status = 'failed'
    OR (tgl.task_id IS NULL AND EXISTS (
      SELECT 1 FROM sync_jobs sj WHERE sj.task_id = t.id AND sj.status = 'failed'
    ))
  );
