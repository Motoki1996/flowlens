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

-- GetGitlabConnectionForOwner/GetGitlabConnectionByIDForOwner only check
-- membership exists (any role), not a specific role: they back both
-- gitlabconn.Service's own owner-only HTTP-facing methods (Get/Test, via
-- getRow) *and* Dial/DialByConnectionID, which internal/linkedproject calls
-- on behalf of a member performing a member-level linked-project action
-- (Create/RegisterWebhook/...). The role-specific check
-- ("is ownerID actually allowed to do *this*") is enforced once, by the
-- caller: gitlabconn.Service.{Save,Get,Test,Delete} call
-- project.Service.Authorize(..., RoleOwner) themselves before touching the
-- row; internal/linkedproject authorizes its own member-minimum before ever
-- calling Dial/DialByConnectionID. Update/Delete below stay owner-only in
-- SQL directly, since nothing else reuses them.

-- name: GetGitlabConnectionForOwner :one
SELECT gc.id, gc.project_id, gc.base_url, gc.encrypted_token, gc.token_gitlab_user_id,
    gc.token_gitlab_username, gc.last_verified_at, gc.last_verify_error, gc.created_at, gc.updated_at
FROM gitlab_connections gc
WHERE gc.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- name: GetGitlabConnectionByIDForOwner :one
-- Same as GetGitlabConnectionForOwner, but keyed by the connection's own ID
-- rather than by its project. internal/linkedproject uses this to dial
-- GitLab (issue #18's webhook registration/repair/delete) starting from a
-- linked_gitlab_projects row, which carries gitlab_connection_id but not the
-- app project ID.
SELECT gc.id, gc.project_id, gc.base_url, gc.encrypted_token, gc.token_gitlab_user_id,
    gc.token_gitlab_username, gc.last_verified_at, gc.last_verify_error, gc.created_at, gc.updated_at
FROM gitlab_connections gc
WHERE gc.id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- name: GetGitlabConnectionByID :one
-- Unscoped, for the outbox worker (internal/issuesync): a linked_gitlab_projects
-- row carries gitlab_connection_id but the worker has no acting user to
-- scope through, the same reasoning as GetLinkedGitlabProjectByID.
SELECT * FROM gitlab_connections WHERE id = $1;

-- name: UpdateGitlabConnectionVerificationForOwner :one
UPDATE gitlab_connections gc
SET token_gitlab_user_id = $2,
    token_gitlab_username = $3,
    last_verified_at = $4,
    last_verify_error = $5,
    updated_at = now()
WHERE gc.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role = 'owner'
  )
RETURNING gc.id, gc.project_id, gc.base_url, gc.encrypted_token, gc.token_gitlab_user_id,
    gc.token_gitlab_username, gc.last_verified_at, gc.last_verify_error, gc.created_at, gc.updated_at;

-- name: DeleteGitlabConnectionForOwner :execrows
DELETE FROM gitlab_connections gc
WHERE gc.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role = 'owner'
  );
