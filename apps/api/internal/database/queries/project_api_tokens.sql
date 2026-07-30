-- project_api_tokens has no owner column of its own; ownership is always
-- checked through the parent project, the same way backlogs, tasks and
-- gitlab_connections are. CreateProjectAPIToken/ListProjectAPITokensByProject
-- trust the caller to have already verified project ownership (e.g. via
-- project.Service.Get), while DeleteProjectAPITokenForOwner joins to projects
-- so a foreign token is indistinguishable from a missing one.
--
-- GetProjectAPITokenByTokenHash is unscoped and used only by bearer
-- authentication (internal/apitoken.Service.Authenticate), which has no
-- acting user to scope through and resolves the project from the token
-- itself. Like GetUserBySessionToken, it filters out expired rows in SQL.

-- name: CreateProjectAPIToken :one
INSERT INTO project_api_tokens (project_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListProjectAPITokensByProject :many
SELECT * FROM project_api_tokens WHERE project_id = $1 ORDER BY created_at DESC;

-- name: DeleteProjectAPITokenForOwner :execrows
DELETE FROM project_api_tokens t
USING projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2;

-- name: GetProjectAPITokenByTokenHash :one
SELECT * FROM project_api_tokens
WHERE token_hash = $1
  AND (expires_at IS NULL OR expires_at > now());

-- name: UpdateProjectAPITokenLastUsedAt :exec
UPDATE project_api_tokens SET last_used_at = $2 WHERE id = $1;
