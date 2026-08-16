-- name: CreateTaskProgressEvent :one
INSERT INTO task_progress_events (task_id, from_progress, to_progress, actor_kind, actor_user_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTaskProgressEventsByTask :many
SELECT * FROM task_progress_events WHERE task_id = $1 ORDER BY occurred_at ASC;
