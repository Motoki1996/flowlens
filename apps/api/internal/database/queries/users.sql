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
