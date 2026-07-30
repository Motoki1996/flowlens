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
USING tasks t, projects p
WHERE td.id = $1 AND t.id = td.predecessor_task_id AND t.project_id = p.id AND p.owner_user_id = $2;
