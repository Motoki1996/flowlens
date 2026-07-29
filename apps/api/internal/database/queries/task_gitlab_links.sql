-- task_gitlab_links has no owner column and is never queried through a
-- project/owner join: only the outbox worker (internal/issuesync) reads and
-- writes it, and the job row that drives it was already authorized when
-- internal/task enqueued it in the same transaction as the task write. See
-- docs/plans/issue-sync.md, "Outbound".

-- name: CreateTaskGitlabLink :one
INSERT INTO task_gitlab_links (
    task_id, linked_gitlab_project_id, gitlab_issue_id, gitlab_issue_iid,
    gitlab_web_url, gitlab_updated_at, last_pushed_fingerprint, sync_status, last_synced_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, 'synced', now()
)
RETURNING *;

-- name: GetTaskGitlabLinkByTaskID :one
SELECT * FROM task_gitlab_links WHERE task_id = $1;

-- name: MarkTaskGitlabLinkSyncedForTask :one
UPDATE task_gitlab_links
SET gitlab_updated_at = $2,
    last_pushed_fingerprint = $3,
    sync_status = 'synced',
    last_error = '',
    last_synced_at = now()
WHERE task_id = $1
RETURNING *;

-- name: MarkTaskGitlabLinkFailedForTask :one
UPDATE task_gitlab_links
SET sync_status = 'failed',
    last_error = $2
WHERE task_id = $1
RETURNING *;
