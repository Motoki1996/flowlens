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

-- SearchProjectMemberCandidates finds users an owner could invite to a
-- project (issue #140): people they already share *some* project with, minus
-- themselves and minus the project's existing members. The whole user table
-- is deliberately out of reach — a searchable directory of every registered
-- account would undo the "no user enumeration" rule the member list follows.
-- Email is neither matched nor returned, for the same reason.
-- name: SearchProjectMemberCandidates :many
SELECT DISTINCT u.id, u.username, u.display_name
FROM users u
JOIN project_members pm ON pm.user_id = u.id
WHERE pm.project_id IN (
        SELECT shared.project_id FROM project_members shared
        WHERE shared.user_id = @caller_user_id
    )
  AND u.id <> @caller_user_id
  AND u.id NOT IN (
        SELECT existing.user_id FROM project_members existing
        WHERE existing.project_id = @project_id
    )
  AND (u.username ILIKE @query OR u.display_name ILIKE @query)
ORDER BY u.username
-- A picker, not a listing: enough rows to choose from, never enough to walk.
LIMIT 10;
