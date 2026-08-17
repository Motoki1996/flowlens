-- name: CreateBacklogProgressEvent :one
INSERT INTO backlog_progress_events (backlog_id, from_progress, to_progress, actor_kind, actor_user_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListBacklogProgressEventsByBacklog :many
SELECT * FROM backlog_progress_events WHERE backlog_id = $1 ORDER BY occurred_at ASC;
