-- webhook_events has no owner column and is never queried through a
-- project/owner join: the linkID in the URL path (POST
-- /webhooks/gitlab/{linkID}) is itself the authorization boundary, verified
-- by comparing X-Gitlab-Token against the link's decrypted webhook secret in
-- constant time before any of these run (see internal/webhookevent,
-- ADR-0008).

-- name: CreateWebhookEvent :one
-- ON CONFLICT DO NOTHING makes a duplicate GitLab delivery (same
-- linked_gitlab_project_id + delivery_uuid) a no-op instead of an error: the
-- caller sees pgx.ErrNoRows and treats it the same as a fresh insert, so
-- retried/duplicated deliveries are idempotent (docs/plans/issue-sync.md,
-- "Inbound").
INSERT INTO webhook_events (
    linked_gitlab_project_id, delivery_uuid, event_name, object_kind,
    gitlab_issue_iid, payload, gitlab_updated_at, status, skip_reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (linked_gitlab_project_id, delivery_uuid) DO NOTHING
RETURNING *;

-- The inbound apply pipeline (internal/webhookapply, docs/plans/issue-sync.md
-- "Inbound"). ClaimNextPendingWebhookEvent must be called through a
-- transaction-scoped Querier (database.TxRunner): FOR UPDATE SKIP LOCKED only
-- holds its lock for the life of the transaction, so the claim and the
-- resulting task write and status update need to commit together — that is
-- what lets a crash mid-apply leave the event 'pending' for a clean retry
-- instead of stuck half-applied. SKIP LOCKED also means two workers polling
-- concurrently never claim the same row. ORDER BY received_at ASC processes
-- deliveries oldest-first, which combined with the stale-event guard means
-- out-of-order redelivery can never apply a newer event before an older one.

-- name: ClaimNextPendingWebhookEvent :one
SELECT * FROM webhook_events
WHERE status = 'pending'
ORDER BY received_at ASC
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: MarkWebhookEventProcessed :exec
UPDATE webhook_events
SET status = 'processed', error_message = '', processed_at = now()
WHERE id = $1;

-- name: MarkWebhookEventSkipped :exec
UPDATE webhook_events
SET status = 'skipped', skip_reason = $2, processed_at = now()
WHERE id = $1;

-- name: MarkWebhookEventFailed :exec
UPDATE webhook_events
SET status = 'failed', error_message = $2, processed_at = now()
WHERE id = $1;
