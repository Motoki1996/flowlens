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
