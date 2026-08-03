-- Every project-scoped query filters on owner_user_id in SQL. Authorization
-- is therefore enforced by the query itself: a non-owner gets "no rows",
-- which callers map to ErrNotFound. Handlers must never do their own
-- ownership check, and later project-scoped tables follow the same rule.

-- name: CreateProject :one
INSERT INTO projects (owner_user_id, name, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjectsByOwner :many
SELECT * FROM projects WHERE owner_user_id = $1 ORDER BY created_at DESC;

-- name: GetProjectForOwner :one
SELECT * FROM projects WHERE id = $1 AND owner_user_id = $2;

-- GetProjectByID is unscoped, for the inbound webhook apply pipeline
-- (internal/webhookapply, docs/plans/issue-sync.md "Inbound"), which
-- resolves a new unclassified task's created_by_user_id from the project's
-- owner and has no acting user of its own to scope through — the same
-- reasoning as GetLinkedGitlabProjectByID.

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;

-- name: UpdateProjectForOwner :one
UPDATE projects
SET name = $3, description = $4, updated_at = now()
WHERE id = $1 AND owner_user_id = $2
RETURNING *;

-- name: DeleteProjectForOwner :execrows
DELETE FROM projects WHERE id = $1 AND owner_user_id = $2;

-- ListFailedSyncProjectsByOwner backs the dashboard's "sync failures"
-- section (issue #77): every project ownerID owns that has at least one
-- task with a failed GitLab sync, counted the same way
-- CountFailedSyncTasksByProjectForOwner counts a single project — from
-- task_gitlab_links when a link exists, or from the task's most recent
-- sync_jobs row otherwise — but joined across every project in one round
-- trip instead of one query per project.

-- name: ListFailedSyncProjectsByOwner :many
SELECT p.*, COUNT(*) AS failed_sync_task_count
FROM projects p
JOIN tasks t ON t.project_id = p.id
LEFT JOIN task_gitlab_links tgl ON tgl.task_id = t.id
WHERE p.owner_user_id = $1
  AND (
    tgl.sync_status = 'failed'
    OR (tgl.task_id IS NULL AND EXISTS (
      SELECT 1 FROM sync_jobs sj WHERE sj.task_id = t.id AND sj.status = 'failed'
    ))
  )
GROUP BY p.id
ORDER BY p.updated_at DESC;
