-- name: UpsertUser :one
INSERT INTO users (
    github_user_id, github_login, display_name, avatar_url, encrypted_access_token
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (github_user_id) DO UPDATE SET
    github_login           = EXCLUDED.github_login,
    display_name           = EXCLUDED.display_name,
    avatar_url             = EXCLUDED.avatar_url,
    encrypted_access_token = EXCLUDED.encrypted_access_token,
    updated_at             = now()
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;
