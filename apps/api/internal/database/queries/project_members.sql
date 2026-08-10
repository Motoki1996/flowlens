-- These back internal/projectmember, the member invite/list/role-change/
-- remove API (issue #100), the first thing to actually write to
-- project_members since it was added by
-- docs/decisions/0010-why-project-membership.md.

-- name: AddProjectMember :one
INSERT INTO project_members (project_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjectMembers :many
SELECT * FROM project_members WHERE project_id = $1 ORDER BY created_at ASC;

-- ListProjectMembersWithUser joins in the username/display name the member
-- list response needs; email is deliberately left out so the response never
-- carries it (issue #100's "avoid user enumeration via email").
-- name: ListProjectMembersWithUser :many
SELECT pm.project_id, pm.user_id, pm.role, pm.created_at, u.username, u.display_name
FROM project_members pm
JOIN users u ON u.id = pm.user_id
WHERE pm.project_id = $1
ORDER BY pm.created_at ASC;

-- name: GetProjectMemberRole :one
SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2;

-- name: UpdateProjectMemberRole :one
UPDATE project_members
SET role = $3
WHERE project_id = $1 AND user_id = $2
RETURNING *;

-- name: RemoveProjectMember :execrows
DELETE FROM project_members WHERE project_id = $1 AND user_id = $2;
