-- Backlogs have no owner column of their own; ownership is always checked
-- through the parent project. CreateBacklog/ListBacklogsByProject trust the
-- caller to have already verified project ownership (e.g. via
-- project.Service.Get), while the single-backlog queries join to projects so
-- a foreign backlog is indistinguishable from a missing one.

-- name: CreateBacklog :one
INSERT INTO backlogs (project_id, name, description, position, start_date, due_on)
VALUES (
    $1,
    $2,
    $3,
    COALESCE((SELECT MAX(position) + 1 FROM backlogs WHERE project_id = $1), 0),
    $4,
    $5
)
RETURNING *;

-- name: ListBacklogsByProject :many
SELECT * FROM backlogs WHERE project_id = $1 ORDER BY position ASC, created_at ASC;

-- name: GetBacklogForOwner :one
SELECT b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at, b.start_date, b.due_on
FROM backlogs b
JOIN projects p ON p.id = b.project_id
WHERE b.id = $1 AND p.owner_user_id = $2;

-- GetBacklogProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-backlog URL, without
-- GetBacklogForOwner's owner join — a token has no session owner to join
-- against.

-- name: GetBacklogProjectID :one
SELECT project_id FROM backlogs WHERE id = $1;

-- UpdateBacklogForOwner overwrites every editable column, so start_date/due_on
-- must arrive already resolved: backlog.Service reads the current row first and
-- fills in whatever the PATCH body left out (see its Update).
-- name: UpdateBacklogForOwner :one
UPDATE backlogs b
SET name = $3, description = $4, position = $5, start_date = $6, due_on = $7, updated_at = now()
FROM projects p
WHERE b.id = $1 AND b.project_id = p.id AND p.owner_user_id = $2
RETURNING b.id, b.project_id, b.name, b.description, b.position, b.created_at, b.updated_at, b.start_date, b.due_on;

-- name: DeleteBacklogForOwner :execrows
DELETE FROM backlogs b
USING projects p
WHERE b.id = $1 AND b.project_id = p.id AND p.owner_user_id = $2;
