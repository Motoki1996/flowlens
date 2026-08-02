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
-- itself. Like GetUserBySessionToken, it filters out expired rows in SQL. It
-- joins projects to also resolve owner_user_id: a bearer request has no
-- session, so the project's owner is the only user it can act as (see
-- internal/apitoken's Auth type).

-- name: CreateProjectAPIToken :one
INSERT INTO project_api_tokens (project_id, name, token_hash, token_prefix, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListProjectAPITokensByProject :many
SELECT * FROM project_api_tokens WHERE project_id = $1 ORDER BY created_at DESC;

-- name: DeleteProjectAPITokenForOwner :execrows
DELETE FROM project_api_tokens t
USING projects p
WHERE t.id = $1 AND t.project_id = p.id AND p.owner_user_id = $2;

-- name: GetProjectAPITokenByTokenHash :one
SELECT t.*, p.owner_user_id
FROM project_api_tokens t
JOIN projects p ON p.id = t.project_id
WHERE t.token_hash = $1
  AND (t.expires_at IS NULL OR t.expires_at > now());

-- name: UpdateProjectAPITokenLastUsedAt :exec
UPDATE project_api_tokens SET last_used_at = $2 WHERE id = $1;
