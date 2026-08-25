-- linked_gitlab_projects has no owner column of its own; ownership is always
-- checked by joining through gitlab_connections to projects, the same way
-- gitlab_connections is checked through projects. A link belonging to
-- another user's project is indistinguishable from a missing one.

-- name: CreateLinkedGitlabProject :one
-- is_default is computed here, not by the caller: the first project linked
-- to a connection becomes its default, later ones do not.
INSERT INTO linked_gitlab_projects (
    gitlab_connection_id, gitlab_project_id, path_with_namespace, name, web_url,
    sync_scope, sync_labels, is_default
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    NOT EXISTS (SELECT 1 FROM linked_gitlab_projects WHERE gitlab_connection_id = $1)
)
RETURNING *;

-- name: ListLinkedGitlabProjectsForOwner :many
SELECT lgp.*
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE gc.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
ORDER BY lgp.created_at;

-- name: GetLinkedGitlabProjectForOwner :one
SELECT lgp.*
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE lgp.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- name: GetLinkedGitlabProjectByID :one
-- Unscoped: the outbox worker (internal/issuesync) has no acting user. The
-- sync_jobs row that names this linked project's ID was already authorized
-- when internal/task enqueued it, so the job's execution needs no further
-- ownership check.
SELECT * FROM linked_gitlab_projects WHERE id = $1;

-- name: GetLinkedGitlabProjectProjectID :one
-- The lightweight project lookup requireTokenResourceProject (internal/http,
-- issue #66) uses to enforce a bearer token's project boundary on
-- GET /linked-gitlab-projects/{linkID}/sync-runs, resolved through
-- gitlab_connections the same way linkedProjectOwner (dbtest) does, since a
-- linked project has no project_id column of its own.
SELECT gc.project_id
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE lgp.id = $1;

-- name: GetLinkedGitlabProjectInProjectForOwner :one
-- internal/backlog uses this to check that the link a backlog names as its own
-- issue destination (000021) really belongs to that backlog's project — a
-- constraint the schema cannot express, since a link reaches its project only
-- through gitlab_connections. A link in another project, or in a project the
-- caller cannot see, comes back as no rows, the same as a missing one.
SELECT lgp.*
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE lgp.id = $1
  AND gc.project_id = $2
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- name: GetEpicLinkedGitlabProjectForOwner :one
-- The epic-scoped rung above GetBacklogLinkedGitlabProjectForOwner (000032):
-- internal/task resolves a new task's issue destination from its epic first,
-- then the epic's backlog, then the project default. Joining through epics
-- keeps the check that the link and the epic share a project inside the query.
SELECT lgp.*
FROM epics e
JOIN linked_gitlab_projects lgp ON lgp.id = e.default_linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE e.id = $1
  AND gc.project_id = e.project_id
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = e.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- name: GetBacklogLinkedGitlabProjectForOwner :one
-- The backlog-scoped half of GetDefaultLinkedGitlabProjectForOwner:
-- internal/task resolves a new task's issue destination from its backlog
-- first (000021), falling back to the project default when the backlog names
-- no link of its own. Joining through backlogs keeps the check that the link
-- and the backlog share a project inside the query.
SELECT lgp.*
FROM backlogs b
JOIN linked_gitlab_projects lgp ON lgp.id = b.default_linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE b.id = $1
  AND gc.project_id = b.project_id
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = b.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- name: GetDefaultLinkedGitlabProjectForOwner :one
-- internal/task uses this at task-create time to decide whether the
-- project has anywhere to push a new issue, and if so, where
-- (docs/plans/issue-sync.md, "Outbound").
SELECT lgp.*
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE gc.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND lgp.is_default = true;

-- Everything below is member-minimum (issue #99): using an already-connected
-- project to link/sync is a member-level action, distinct from managing the
-- connection's credential itself (owner-only, gitlab_connections.sql).

-- name: UpdateLinkedGitlabProjectSyncScopeForOwner :one
UPDATE linked_gitlab_projects lgp
SET sync_scope = $2,
    sync_labels = $3,
    updated_at = now()
FROM gitlab_connections gc
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id
    AND EXISTS (
      SELECT 1 FROM project_members pm
      WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
    )
RETURNING lgp.*;

-- name: ClearDefaultLinkedGitlabProjectsForOwner :exec
-- Unsets is_default on every other link in the same connection as linkID,
-- so SetDefaultLinkedGitlabProjectForOwner can set exactly one.
UPDATE linked_gitlab_projects lgp
SET is_default = false,
    updated_at = now()
FROM gitlab_connections gc
WHERE lgp.gitlab_connection_id = gc.id
    AND EXISTS (
      SELECT 1 FROM project_members pm
      WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
    )
    AND lgp.gitlab_connection_id = (
        SELECT orig.gitlab_connection_id FROM linked_gitlab_projects orig WHERE orig.id = $1
    )
    AND lgp.id != $1;

-- name: SetDefaultLinkedGitlabProjectForOwner :one
UPDATE linked_gitlab_projects lgp
SET is_default = true,
    updated_at = now()
FROM gitlab_connections gc
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id
    AND EXISTS (
      SELECT 1 FROM project_members pm
      WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
    )
RETURNING lgp.*;

-- name: DeleteLinkedGitlabProjectForOwner :one
-- Returns the deleted row (rather than an affected-row count) so the
-- service can tell whether it removed the default link and needs to
-- promote another one.
DELETE FROM linked_gitlab_projects lgp
USING gitlab_connections gc
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id
    AND EXISTS (
      SELECT 1 FROM project_members pm
      WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
    )
RETURNING lgp.*;

-- name: PromoteOldestLinkedGitlabProjectAsDefault :exec
-- Used after deleting the default link: makes the oldest remaining link in
-- the same connection the new default. A no-op if none remain.
UPDATE linked_gitlab_projects
SET is_default = true, updated_at = now()
WHERE id = (
    SELECT candidate.id FROM linked_gitlab_projects candidate
    WHERE candidate.gitlab_connection_id = $1
    ORDER BY candidate.created_at
    LIMIT 1
);

-- name: SetLinkedGitlabProjectWebhookForOwner :one
-- Records a successful webhook registration or rotation (issue #18) and
-- clears any earlier registration error.
UPDATE linked_gitlab_projects lgp
SET webhook_id = $2,
    encrypted_webhook_secret = $3,
    webhook_registered_at = now(),
    webhook_registration_error = '',
    updated_at = now()
FROM gitlab_connections gc
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id
    AND EXISTS (
      SELECT 1 FROM project_members pm
      WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
    )
RETURNING lgp.*;

-- name: SetLinkedGitlabProjectWebhookErrorForOwner :one
-- Records why registering or repairing a webhook failed (most commonly
-- insufficient GitLab permissions) without touching any existing
-- webhook_id, so the link stays usable via manual sync.
UPDATE linked_gitlab_projects lgp
SET webhook_registration_error = $2,
    updated_at = now()
FROM gitlab_connections gc
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id
    AND EXISTS (
      SELECT 1 FROM project_members pm
      WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
    )
RETURNING lgp.*;

-- name: UpdateLinkedGitlabProjectLastSyncedAt :one
-- Unscoped, like GetLinkedGitlabProjectByID: called by the background
-- worker (internal/projectsync) after a project.import/project.resync run
-- finishes, which has no acting user to scope through.
UPDATE linked_gitlab_projects
SET last_synced_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateLinkedGitlabProjectInitialImportStatus :one
-- Unscoped, for the same reason as UpdateLinkedGitlabProjectLastSyncedAt.
-- Values used: "pending" (default), "completed", "failed"
-- (internal/projectsync).
UPDATE linked_gitlab_projects
SET initial_import_status = $2, updated_at = now()
WHERE id = $1
RETURNING *;
