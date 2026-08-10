-- task_dependencies has no project_id/owner column of its own; ownership is
-- always checked through its tasks, the same way task_gitlab_links is never
-- queried through a project/owner join directly. CreateTaskDependency and
-- ListTaskDependenciesByProject trust the caller (internal/taskdependency) to
-- have already verified project ownership and that both tasks belong to the
-- same project, the same way CreateTask trusts an already-verified project.
-- DeleteTaskDependencyForOwner joins through the predecessor task to
-- projects, so a dependency belonging to another user's project is
-- indistinguishable from a missing one.

-- name: CreateTaskDependency :one
INSERT INTO task_dependencies (predecessor_task_id, successor_task_id)
VALUES ($1, $2)
RETURNING id, predecessor_task_id, successor_task_id, created_at;

-- name: ListTaskDependenciesByProject :many
SELECT td.id, td.predecessor_task_id, td.successor_task_id, td.created_at
FROM task_dependencies td
JOIN tasks t ON t.id = td.predecessor_task_id
WHERE t.project_id = $1
ORDER BY td.created_at ASC;

-- name: DeleteTaskDependencyForOwner :execrows
DELETE FROM task_dependencies td
USING tasks t
WHERE td.id = $1 AND t.id = td.predecessor_task_id
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = t.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- GetTaskDependencyProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-dependency URL: resolved
-- through the predecessor task, the same way DeleteTaskDependencyForOwner
-- resolves ownership.

-- name: GetTaskDependencyProjectID :one
SELECT t.project_id
FROM task_dependencies td
JOIN tasks t ON t.id = td.predecessor_task_id
WHERE td.id = $1;
