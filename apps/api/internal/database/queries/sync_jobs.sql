-- The outbox worker (internal/sync, docs/plans/issue-sync.md "Sync engine").
--
-- EnqueueSyncJob upserts on dedupe_key: a colliding insert only overwrites the
-- existing row while it is still 'pending' (collapsing rapid repeated edits
-- into one job); a collision with a running/succeeded/failed row leaves that
-- row untouched and returns no rows, so the caller falls back to
-- GetSyncJobByDedupeKey and reuses it as-is. MarkSyncJobSucceeded and
-- MarkSyncJobFailed clear dedupe_key so a later edit is never permanently
-- blocked by a job that already reached a terminal state.
--
-- ClaimPendingSyncJobs is the SKIP LOCKED claim: it atomically selects and
-- flips due pending jobs to 'running' in one statement, so two workers
-- polling concurrently never claim the same row.

-- name: EnqueueSyncJob :one
INSERT INTO sync_jobs (project_id, task_id, kind, payload, dedupe_key)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (dedupe_key) DO UPDATE
SET payload = EXCLUDED.payload, updated_at = now()
WHERE sync_jobs.status = 'pending'
RETURNING id, project_id, task_id, kind, payload, dedupe_key, status, attempts, run_after, last_error, created_at, updated_at;

-- name: GetSyncJobByDedupeKey :one
SELECT id, project_id, task_id, kind, payload, dedupe_key, status, attempts, run_after, last_error, created_at, updated_at
FROM sync_jobs
WHERE dedupe_key = $1;

-- name: ClaimPendingSyncJobs :many
WITH claimed AS (
    SELECT id FROM sync_jobs
    WHERE status = 'pending' AND run_after <= now()
    ORDER BY run_after ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE sync_jobs
SET status = 'running', updated_at = now()
WHERE id IN (SELECT id FROM claimed)
RETURNING id, project_id, task_id, kind, payload, dedupe_key, status, attempts, run_after, last_error, created_at, updated_at;

-- name: MarkSyncJobSucceeded :exec
UPDATE sync_jobs
SET status = 'succeeded', dedupe_key = NULL, last_error = '', updated_at = now()
WHERE id = $1;

-- name: MarkSyncJobRetry :exec
UPDATE sync_jobs
SET status = 'pending', attempts = attempts + 1, run_after = $2, last_error = $3, updated_at = now()
WHERE id = $1;

-- name: MarkSyncJobFailed :exec
UPDATE sync_jobs
SET status = 'failed', attempts = attempts + 1, dedupe_key = NULL, last_error = $2, updated_at = now()
WHERE id = $1;

-- name: ReclaimStaleRunningSyncJobs :execrows
UPDATE sync_jobs
SET status = 'pending', updated_at = now()
WHERE status = 'running' AND updated_at < $1;

-- GetLatestSyncJobForTask backs a task's sync status (internal/task) when it
-- has no task_gitlab_links row yet — i.e. its issue.create job hasn't
-- succeeded (or has permanently failed), so there is nothing in
-- task_gitlab_links to read sync_status from. Before a link exists, a task
-- can only ever have enqueued issue.create jobs (issue.update/close/reopen
-- all require an existing link), so "latest" is unambiguous.

-- name: GetLatestSyncJobForTask :one
SELECT id, project_id, task_id, kind, payload, dedupe_key, status, attempts, run_after, last_error, created_at, updated_at
FROM sync_jobs
WHERE task_id = sqlc.arg(task_id)::uuid
ORDER BY created_at DESC
LIMIT 1;

-- RetryFailedSyncJobForTask powers POST /tasks/{taskID}/sync-retry: it
-- forces the task's most recent pending-or-failed job to run again
-- immediately with a fresh attempt budget. 'pending' is included alongside
-- 'failed' because task_gitlab_links.sync_status can already read 'failed'
-- (set on the first failed push attempt) while the job itself is still
-- mid-backoff, not yet exhausted — internal/task.Service.RetrySync checks
-- that sync_status first, so by the time this query runs the caller already
-- knows a retry is warranted. Returns no rows if the task has no
-- pending/failed job (e.g. it never had one, or it already succeeded),
-- which internal/task maps to ErrSyncNotFailed.

-- name: RetryFailedSyncJobForTask :one
UPDATE sync_jobs
SET status = 'pending', attempts = 0, run_after = now(), last_error = ''
WHERE id = (
    SELECT id FROM sync_jobs
    WHERE task_id = sqlc.arg(task_id)::uuid AND status IN ('pending', 'failed')
    ORDER BY created_at DESC
    LIMIT 1
)
RETURNING id, project_id, task_id, kind, payload, dedupe_key, status, attempts, run_after, last_error, created_at, updated_at;

-- GetPendingSyncJobQueueStats backs the worker's queue-depth gauges (issue
-- #96): how many jobs are waiting, and how long the oldest of them has been
-- waiting, are what turn "the worker looks busy" into "the worker is stuck".
-- oldest_pending_at is NULL (via min() on zero rows) when the queue is
-- empty, which internal/sync reads as "no gauge to report" rather than a
-- bogus zero-age job.

-- name: GetPendingSyncJobQueueStats :one
SELECT
    count(*)::bigint AS pending_count,
    min(created_at)::timestamptz AS oldest_pending_at
FROM sync_jobs
WHERE status = 'pending';
