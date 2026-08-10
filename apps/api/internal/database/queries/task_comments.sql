-- name: CreateTaskComment :one
INSERT INTO task_comments (task_id, author_user_id, author_token_id, author_kind, body)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTaskCommentsByTask :many
SELECT * FROM task_comments WHERE task_id = $1 ORDER BY created_at ASC;

-- name: GetTaskCommentByID :one
SELECT * FROM task_comments WHERE id = $1;

-- name: DeleteTaskCommentByID :execrows
DELETE FROM task_comments WHERE id = $1;

-- GetTaskCommentProjectID is the lightweight project lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-comment URL, resolved through
-- the comment's task, the same way GetTaskDependencyProjectID resolves
-- through a dependency's predecessor task.

-- name: GetTaskCommentProjectID :one
SELECT t.project_id
FROM task_comments tc
JOIN tasks t ON t.id = tc.task_id
WHERE tc.id = $1;
