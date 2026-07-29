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
