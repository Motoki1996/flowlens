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

-- GetProjectAssigneeGitlabIdentity is the one-way assignee bridge's lookup
-- (000031): given a project and the FlowLens user being assigned to one of
-- its tasks, resolve that user's GitLab identity *on the instance this
-- project is connected to*. internal/task uses it to set
-- assignee_gitlab_user_id alongside assignee_user_id, which is what puts the
-- assignment on the GitLab issue. No rows means the assignment stays
-- FlowLens-only: either the project has no GitLab connection, or the user
-- has not registered an identity for that base_url. Both are ordinary, not
-- errors. The equality join on base_url is why 000030 normalized it.

-- name: GetProjectAssigneeGitlabIdentity :one
SELECT ugi.gitlab_user_id, ugi.gitlab_username
FROM gitlab_connections gc
JOIN user_gitlab_identities ugi
  ON ugi.gitlab_base_url = gc.base_url AND ugi.user_id = $2
WHERE gc.project_id = $1;
