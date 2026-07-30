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

-- The troubleshooting read side (issue #26): ListWebhookEventsByLinkedGitlabProjectID
-- and GetWebhookEventByLinkedGitlabProjectIDAndID are scoped by
-- linked_gitlab_project_id only, not owner — internal/webhookevent.Service
-- verifies the caller owns linkID via GetLinkedGitlabProjectForOwner first, the
-- same two-step internal/projectsync's ListRuns uses, so a foreign or unknown
-- eventID under an owned link and an owned eventID under a foreign link both
-- come back empty.

-- name: ListWebhookEventsByLinkedGitlabProjectID :many
-- status = '' disables the filter, matching ListTasksByProject's convention.
SELECT * FROM webhook_events
WHERE linked_gitlab_project_id = sqlc.arg(linked_gitlab_project_id)
  AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status))
ORDER BY received_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- name: GetWebhookEventByLinkedGitlabProjectIDAndID :one
-- The detail view: unlike the list row, its payload is actually read by the
-- caller (internal/http keeps the list DTO payload-free to avoid exposing
-- raw GitLab payloads by default).
SELECT * FROM webhook_events
WHERE id = $1 AND linked_gitlab_project_id = $2;

-- name: RetryWebhookEvent :one
-- Only a 'failed' event can be retried. internal/webhookevent.Service checks
-- the event exists and is 'failed' via GetWebhookEventByLinkedGitlabProjectIDAndID
-- first, so a zero-row result here is only the rare race where another
-- request already retried it; that maps to the same ErrNotFailed.
UPDATE webhook_events
SET status = 'pending', error_message = '', processed_at = NULL
WHERE id = $1 AND status = 'failed'
RETURNING *;

-- name: DeleteProcessedWebhookEventsOlderThan :execrows
-- The retention policy decided for issue #26: only 'processed' rows are
-- pruned. 'failed' and 'skipped' rows are kept indefinitely (their error and
-- skip_reason are the whole point of troubleshooting) and 'pending' rows are
-- never touched by cleanup — the apply worker is the only thing that moves a
-- pending row out of that state.
DELETE FROM webhook_events WHERE status = 'processed' AND processed_at < $1;
