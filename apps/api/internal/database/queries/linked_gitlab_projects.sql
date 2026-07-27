-- linked_gitlab_projects has no owner column of its own; ownership is always
-- checked by joining through gitlab_connections to projects, the same way
-- gitlab_connections is checked through projects. A link belonging to
-- another user's project is indistinguishable from a missing one.

-- name: CreateLinkedGitlabProject :one
-- is_default is computed here, not by the caller: the first project linked
-- to a connection becomes its default, later ones do not.
INSERT INTO linked_gitlab_projects (
    gitlab_connection_id, gitlab_project_id, path_with_namespace, name, web_url,
    sync_scope, sync_labels, is_default
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    NOT EXISTS (SELECT 1 FROM linked_gitlab_projects WHERE gitlab_connection_id = $1)
)
RETURNING *;

-- name: ListLinkedGitlabProjectsForOwner :many
SELECT lgp.*
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
JOIN projects p ON p.id = gc.project_id
WHERE p.id = $1 AND p.owner_user_id = $2
ORDER BY lgp.created_at;

-- name: GetLinkedGitlabProjectForOwner :one
SELECT lgp.*
FROM linked_gitlab_projects lgp
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
JOIN projects p ON p.id = gc.project_id
WHERE lgp.id = $1 AND p.owner_user_id = $2;

-- name: UpdateLinkedGitlabProjectSyncScopeForOwner :one
UPDATE linked_gitlab_projects lgp
SET sync_scope = $3,
    sync_labels = $4,
    updated_at = now()
FROM gitlab_connections gc, projects p
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id AND gc.project_id = p.id
    AND p.owner_user_id = $2
RETURNING lgp.*;

-- name: ClearDefaultLinkedGitlabProjectsForOwner :exec
-- Unsets is_default on every other link in the same connection as linkID,
-- so SetDefaultLinkedGitlabProjectForOwner can set exactly one.
UPDATE linked_gitlab_projects lgp
SET is_default = false,
    updated_at = now()
FROM gitlab_connections gc, projects p
WHERE lgp.gitlab_connection_id = gc.id AND gc.project_id = p.id
    AND p.owner_user_id = $2
    AND lgp.gitlab_connection_id = (
        SELECT orig.gitlab_connection_id FROM linked_gitlab_projects orig WHERE orig.id = $1
    )
    AND lgp.id != $1;

-- name: SetDefaultLinkedGitlabProjectForOwner :one
UPDATE linked_gitlab_projects lgp
SET is_default = true,
    updated_at = now()
FROM gitlab_connections gc, projects p
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id AND gc.project_id = p.id
    AND p.owner_user_id = $2
RETURNING lgp.*;

-- name: DeleteLinkedGitlabProjectForOwner :one
-- Returns the deleted row (rather than an affected-row count) so the
-- service can tell whether it removed the default link and needs to
-- promote another one.
DELETE FROM linked_gitlab_projects lgp
USING gitlab_connections gc, projects p
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id AND gc.project_id = p.id
    AND p.owner_user_id = $2
RETURNING lgp.*;

-- name: PromoteOldestLinkedGitlabProjectAsDefault :exec
-- Used after deleting the default link: makes the oldest remaining link in
-- the same connection the new default. A no-op if none remain.
UPDATE linked_gitlab_projects
SET is_default = true, updated_at = now()
WHERE id = (
    SELECT candidate.id FROM linked_gitlab_projects candidate
    WHERE candidate.gitlab_connection_id = $1
    ORDER BY candidate.created_at
    LIMIT 1
);

-- name: SetLinkedGitlabProjectWebhookForOwner :one
-- Records a successful webhook registration or rotation (issue #18) and
-- clears any earlier registration error.
UPDATE linked_gitlab_projects lgp
SET webhook_id = $3,
    encrypted_webhook_secret = $4,
    webhook_registered_at = now(),
    webhook_registration_error = '',
    updated_at = now()
FROM gitlab_connections gc, projects p
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id AND gc.project_id = p.id
    AND p.owner_user_id = $2
RETURNING lgp.*;

-- name: SetLinkedGitlabProjectWebhookErrorForOwner :one
-- Records why registering or repairing a webhook failed (most commonly
-- insufficient GitLab permissions) without touching any existing
-- webhook_id, so the link stays usable via manual sync.
UPDATE linked_gitlab_projects lgp
SET webhook_registration_error = $3,
    updated_at = now()
FROM gitlab_connections gc, projects p
WHERE lgp.id = $1 AND lgp.gitlab_connection_id = gc.id AND gc.project_id = p.id
    AND p.owner_user_id = $2
RETURNING lgp.*;
