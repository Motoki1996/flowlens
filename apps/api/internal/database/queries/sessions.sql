-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserBySessionToken :one
SELECT sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = $1
  AND sessions.expires_at > now();

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();

-- DeleteSessionsByUserID revokes every session a user holds. A password
-- change does this (issue #210) and then issues a fresh session, so that a
-- session stolen elsewhere is cut and the caller's own token is rotated
-- rather than surviving the change.
-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = $1;
