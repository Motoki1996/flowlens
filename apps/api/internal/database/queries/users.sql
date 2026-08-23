-- name: CreateUser :one
INSERT INTO users (
    username, email, display_name, password_hash
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetUserByUsernameOrEmail :one
SELECT * FROM users WHERE username = $1 OR email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;

-- ListUsersByIDs resolves a batch of user IDs to the username/display name a
-- response needs to render an assignee (000031). Batched rather than joined
-- into the task/backlog queries themselves: a computed column would push
-- every one of those queries off the plain db.Task/db.Backlog row type and
-- into a per-query Row struct, for a lookup that is one extra round trip per
-- list. Like ListProjectMembersWithUser it deliberately omits email.

-- name: ListUsersByIDs :many
SELECT id, username, display_name FROM users WHERE id = ANY(sqlc.arg(ids)::uuid[]);
