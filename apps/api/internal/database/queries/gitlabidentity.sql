-- user_gitlab_identities maps a FlowLens user to their GitLab user ID/username
-- on one GitLab CE instance (gitlab_base_url), so ?assignee=me on the task
-- collections (internal/task) can match against tasks.assignee_gitlab_user_id.
-- UpsertUserGitlabIdentity is the only write: a user re-registering the same
-- base_url replaces the previous mapping rather than erroring.

-- name: UpsertUserGitlabIdentity :one
INSERT INTO user_gitlab_identities (user_id, gitlab_base_url, gitlab_user_id, gitlab_username)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, gitlab_base_url) DO UPDATE
SET gitlab_user_id = EXCLUDED.gitlab_user_id,
    gitlab_username = EXCLUDED.gitlab_username,
    updated_at = now()
RETURNING id, user_id, gitlab_base_url, gitlab_user_id, gitlab_username, created_at, updated_at;

-- name: ListUserGitlabIdentitiesByUser :many
SELECT id, user_id, gitlab_base_url, gitlab_user_id, gitlab_username, created_at, updated_at
FROM user_gitlab_identities
WHERE user_id = $1
ORDER BY gitlab_base_url ASC;
