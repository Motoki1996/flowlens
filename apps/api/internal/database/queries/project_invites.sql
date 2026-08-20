-- name: CreateProjectInvite :one
INSERT INTO project_invites (
    project_id, token_hash, token_prefix, role, expires_at, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListProjectInvitesByProject :many
SELECT * FROM project_invites
WHERE project_id = $1
ORDER BY created_at DESC;

-- GetProjectInviteByTokenHash resolves a raw invite token (hashed by the
-- caller) to the invite it names, joined to the project it grants access
-- to so the acceptance screen can name it without a second query. It
-- deliberately does not filter on expires_at or accepted_at: the caller
-- distinguishes "expired" from "already used" from "never existed" for its
-- own error, and only the last of those is a not-found.
-- name: GetProjectInviteByTokenHash :one
SELECT sqlc.embed(project_invites), projects.name AS project_name
FROM project_invites
JOIN projects ON projects.id = project_invites.project_id
WHERE project_invites.token_hash = $1;

-- AcceptProjectInvite spends an invite, and only if it is still spendable:
-- the accepted_at IS NULL guard means two callers racing to accept the same
-- invite cannot both win, since the loser's UPDATE matches no row.
-- name: AcceptProjectInvite :one
UPDATE project_invites
SET accepted_at = now(), accepted_by_user_id = $2
WHERE id = $1 AND accepted_at IS NULL AND expires_at > now()
RETURNING *;

-- DeleteProjectInviteForOwner revokes an invite, scoped to a caller holding
-- the 'owner' role on its project — the ownership check is in the query, so
-- a non-owner deletes nothing and cannot tell the invite apart from a
-- missing one. Mirrors DeleteProjectAPITokenForOwner.
-- name: DeleteProjectInviteForOwner :execrows
DELETE FROM project_invites
WHERE id = $1
  AND project_id IN (
      SELECT project_id FROM project_members
      WHERE user_id = $2 AND role = 'owner'
  );
