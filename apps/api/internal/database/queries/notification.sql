-- Daily digest notifications (issue #109). notification_settings has no
-- owner column of its own; the owner-scoped queries join through
-- project_members the same way gitlab_connections does. The digest worker
-- itself has no acting user (like sync.Worker), so ListEnabledNotificationSettings
-- and the digest-content queries below are unscoped.

-- name: UpsertNotificationSettings :one
INSERT INTO notification_settings (project_id, webhook_url, enabled, send_hour)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id) DO UPDATE
SET webhook_url = EXCLUDED.webhook_url,
    enabled = EXCLUDED.enabled,
    send_hour = EXCLUDED.send_hour,
    updated_at = now()
RETURNING *;

-- name: GetNotificationSettingsForOwner :one
SELECT ns.*
FROM notification_settings ns
WHERE ns.project_id = $1
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = ns.project_id AND pm.user_id = sqlc.arg(owner_user_id) AND pm.role = 'owner'
  );

-- ListEnabledNotificationSettings backs the digest worker's sweep: every
-- project with notifications turned on, regardless of caller, since the
-- worker runs outside any request.

-- name: ListEnabledNotificationSettings :many
SELECT * FROM notification_settings WHERE enabled = true;

-- InsertNotificationDigestLog is the dedupe guard itself: the worker calls
-- this *before* building or sending a digest, so a second attempt for the
-- same project on the same day (a slow first attempt still in flight, or a
-- second process) hits the notification_digests unique constraint and
-- reports pgx.ErrNoRows instead of a duplicate send.

-- name: InsertNotificationDigestLog :one
INSERT INTO notification_digests (project_id, digest_date, status, error)
VALUES ($1, $2, $3, $4)
ON CONFLICT (project_id, digest_date) DO NOTHING
RETURNING *;

-- name: MarkNotificationDigestFailed :exec
UPDATE notification_digests
SET status = 'failed', error = $2
WHERE id = $1;

-- ListOverdueOpenTasksByProject / ListTasksDueSoonByProject back the digest
-- content (issue #109's (a)/(b)). due_on is a DATE (no time-of-day), so
-- "due within 24h" is approximated as "due today" — the finest-grained
-- distinction the column supports.

-- name: ListOverdueOpenTasksByProject :many
SELECT * FROM tasks
WHERE project_id = $1 AND status = 'open' AND due_on IS NOT NULL AND due_on < sqlc.arg(today)::date
ORDER BY due_on ASC;

-- name: ListTasksDueSoonByProject :many
SELECT * FROM tasks
WHERE project_id = $1 AND status = 'open' AND due_on = sqlc.arg(today)::date
ORDER BY due_on ASC;

-- name: ListFailedSyncJobsByProject :many
SELECT * FROM sync_jobs
WHERE project_id = $1 AND status = 'failed'
ORDER BY updated_at DESC;

-- name: ListFailedWebhookEventsByProject :many
SELECT we.*
FROM webhook_events we
JOIN linked_gitlab_projects lgp ON lgp.id = we.linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE gc.project_id = $1 AND we.status = 'failed'
ORDER BY we.received_at DESC;
