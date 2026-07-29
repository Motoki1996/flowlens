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
