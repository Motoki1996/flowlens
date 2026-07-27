-- gitlab_connections has no owner column of its own; ownership is always
-- checked through the parent project, the same way backlogs and tasks are.
-- UpsertGitlabConnection trusts the caller to have already verified project
-- ownership (e.g. via project.Service.Get) before running, while the other
-- queries join to projects so a foreign connection is indistinguishable from
-- a missing one. The access token is only ever handled encrypted here; see
-- internal/crypto and internal/gitlabconn.

-- name: UpsertGitlabConnection :one
INSERT INTO gitlab_connections (
    project_id, base_url, encrypted_token,
    token_gitlab_user_id, token_gitlab_username, last_verified_at, last_verify_error
)
VALUES ($1, $2, $3, $4, $5, now(), '')
ON CONFLICT (project_id) DO UPDATE
SET base_url = EXCLUDED.base_url,
    encrypted_token = EXCLUDED.encrypted_token,
    token_gitlab_user_id = EXCLUDED.token_gitlab_user_id,
    token_gitlab_username = EXCLUDED.token_gitlab_username,
    last_verified_at = now(),
    last_verify_error = '',
    updated_at = now()
RETURNING *;

-- name: GetGitlabConnectionForOwner :one
SELECT gc.id, gc.project_id, gc.base_url, gc.encrypted_token, gc.token_gitlab_user_id,
    gc.token_gitlab_username, gc.last_verified_at, gc.last_verify_error, gc.created_at, gc.updated_at
FROM gitlab_connections gc
JOIN projects p ON p.id = gc.project_id
WHERE gc.project_id = $1 AND p.owner_user_id = $2;

-- name: UpdateGitlabConnectionVerificationForOwner :one
UPDATE gitlab_connections gc
SET token_gitlab_user_id = $3,
    token_gitlab_username = $4,
    last_verified_at = $5,
    last_verify_error = $6,
    updated_at = now()
FROM projects p
WHERE gc.project_id = $1 AND gc.project_id = p.id AND p.owner_user_id = $2
RETURNING gc.id, gc.project_id, gc.base_url, gc.encrypted_token, gc.token_gitlab_user_id,
    gc.token_gitlab_username, gc.last_verified_at, gc.last_verify_error, gc.created_at, gc.updated_at;

-- name: DeleteGitlabConnectionForOwner :execrows
DELETE FROM gitlab_connections gc
USING projects p
WHERE gc.project_id = $1 AND gc.project_id = p.id AND p.owner_user_id = $2;
