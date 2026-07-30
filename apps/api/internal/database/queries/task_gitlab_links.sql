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

-- GetTaskGitlabLinkWithProjectPathByTaskID is GetTaskGitlabLinkByTaskID plus
-- the linked GitLab project's path_with_namespace, joined in for the
-- AI-facing /context endpoints (docs/plans/issue-sync.md "AI-facing"): an AI
-- agent needs the project path alongside the issue IID/URL to identify the
-- issue without a second call.

-- name: GetTaskGitlabLinkWithProjectPathByTaskID :one
SELECT tgl.task_id, tgl.linked_gitlab_project_id, tgl.gitlab_issue_id, tgl.gitlab_issue_iid, tgl.gitlab_web_url, tgl.gitlab_updated_at, tgl.last_pushed_fingerprint, tgl.sync_status, tgl.last_error, tgl.last_synced_at, lgp.path_with_namespace
FROM task_gitlab_links tgl
JOIN linked_gitlab_projects lgp ON lgp.id = tgl.linked_gitlab_project_id
WHERE tgl.task_id = $1;

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

-- MarkTaskGitlabLinkPendingForTask is the other half of a sync retry
-- (internal/task.Service.RetrySync, alongside RetryFailedSyncJobForTask in
-- sync_jobs.sql): it puts an already-linked task's sync status back to
-- 'pending' so the UI reflects the retry immediately, without waiting for
-- the worker to pick the job back up. A task whose issue.create never
-- succeeded has no row here yet, so a no-match (no rows returned) is
-- expected and not an error — the caller falls back to sync_jobs alone for
-- that case.

-- name: MarkTaskGitlabLinkPendingForTask :one
UPDATE task_gitlab_links
SET sync_status = 'pending',
    last_error = ''
WHERE task_id = $1
RETURNING *;

-- GetTaskGitlabLinkByLinkedProjectAndIID looks up a task already linked to a
-- specific GitLab issue, keyed by the same columns as the 1:1 UNIQUE
-- constraint. The inbound apply pipeline (internal/webhookapply,
-- docs/plans/issue-sync.md "Inbound") uses this to tell a known issue
-- (update an existing task) from an unknown one (create a new unclassified
-- task).

-- name: GetTaskGitlabLinkByLinkedProjectAndIID :one
SELECT * FROM task_gitlab_links
WHERE linked_gitlab_project_id = $1 AND gitlab_issue_iid = $2;

-- MarkTaskGitlabLinkAppliedForTask records a successful inbound apply
-- (internal/webhookapply): only gitlab_updated_at advances and
-- sync_status/last_error clear. Unlike MarkTaskGitlabLinkSyncedForTask (the
-- outbound counterpart), it never touches last_pushed_fingerprint — that
-- field records what FlowLens itself last pushed, and an inbound apply is by
-- definition not that.

-- name: MarkTaskGitlabLinkAppliedForTask :one
UPDATE task_gitlab_links
SET gitlab_updated_at = $2,
    sync_status = 'synced',
    last_error = ''
WHERE task_id = $1
RETURNING *;
