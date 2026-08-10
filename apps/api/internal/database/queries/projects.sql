-- Every project-scoped query filters on project_members in SQL (see
-- docs/decisions/0010-why-project-membership.md and issue #99): access is
-- therefore enforced by the query itself, not by application code. A
-- read query (Get/List) accepts any role; a write query restricts the
-- EXISTS subquery's role IN (...) to the roles that action requires. A
-- caller with no membership row at all gets "no rows", which callers map to
-- ErrNotFound; one with a membership row of too low a role also gets "no
-- rows" from these queries alone — the distinct ErrForbidden comes from
-- project.Service.Authorize (backed by GetProjectMemberRole), not from
-- these list/get/write queries. DeleteProjectForOwner is the one exception:
-- it stays keyed on projects.owner_user_id directly, not project_members,
-- per the ADR's "last owner" reasoning.

-- name: CreateProject :one
INSERT INTO projects (owner_user_id, name, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjectsByMember :many
SELECT p.* FROM projects p
JOIN project_members pm ON pm.project_id = p.id
WHERE pm.user_id = $1
ORDER BY p.created_at DESC;

-- name: GetProjectForOwner :one
SELECT * FROM projects
WHERE id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = projects.id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- GetProjectByID is unscoped, for the inbound webhook apply pipeline
-- (internal/webhookapply, docs/plans/issue-sync.md "Inbound"), which
-- resolves a new unclassified task's created_by_user_id from the project's
-- owner and has no acting user of its own to scope through — the same
-- reasoning as GetLinkedGitlabProjectByID.

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;

-- UpdateProjectForOwner backs project.Service.Update (name/description
-- edit), member-minimum per issue #99 — not explicitly named owner-only, so
-- it follows the "writes are member-tier by default" rule.

-- name: UpdateProjectForOwner :one
UPDATE projects
SET name = $2, description = $3, updated_at = now()
WHERE id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = projects.id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role IN ('member', 'owner')
  )
RETURNING *;

-- DeleteProjectForOwner deliberately stays keyed on the single
-- owner_user_id column, not a project_members role check: restricting
-- delete to the one designated owner avoids the "last owner deletes, a
-- second owner-role member is left holding a project with no owner" race a
-- membership table alone invites (docs/decisions/0010-why-project-membership.md).

-- name: DeleteProjectForOwner :execrows
DELETE FROM projects WHERE id = $1 AND owner_user_id = $2;

-- ListFailedSyncProjectsByMember backs the dashboard's "sync failures"
-- section (issue #77): every project ownerID is a member of (any role) that
-- has at least one task with a failed GitLab sync, counted the same way
-- CountFailedSyncTasksByProjectForOwner counts a single project — from
-- task_gitlab_links when a link exists, or from the task's most recent
-- sync_jobs row otherwise — but joined across every project in one round
-- trip instead of one query per project.

-- name: ListFailedSyncProjectsByMember :many
SELECT p.*, COUNT(*) AS failed_sync_task_count
FROM projects p
JOIN project_members pm ON pm.project_id = p.id
JOIN tasks t ON t.project_id = p.id
LEFT JOIN task_gitlab_links tgl ON tgl.task_id = t.id
WHERE pm.user_id = $1
  AND (
    tgl.sync_status = 'failed'
    OR (tgl.task_id IS NULL AND EXISTS (
      SELECT 1 FROM sync_jobs sj WHERE sj.task_id = t.id AND sj.status = 'failed'
    ))
  )
GROUP BY p.id
ORDER BY p.updated_at DESC;
