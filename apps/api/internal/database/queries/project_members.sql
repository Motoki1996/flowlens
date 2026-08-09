-- project_members has no queries wired into any handler yet (see
-- docs/decisions/0010-why-project-membership.md); these exist so the schema
-- and the backfill are exercised by a real query, ahead of the
-- ownership-check replacement in a follow-up issue.

-- name: AddProjectMember :one
INSERT INTO project_members (project_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjectMembers :many
SELECT * FROM project_members WHERE project_id = $1 ORDER BY created_at ASC;

-- name: GetProjectMemberRole :one
SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2;

-- name: UpdateProjectMemberRole :one
UPDATE project_members
SET role = $3
WHERE project_id = $1 AND user_id = $2
RETURNING *;

-- name: RemoveProjectMember :execrows
DELETE FROM project_members WHERE project_id = $1 AND user_id = $2;
