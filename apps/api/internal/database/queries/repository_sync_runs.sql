-- repository_sync_runs records one mr.import / mr.resync execution against a
-- repository (ADR-0011 §2: reuses gitlab_sync_runs' shape, scoped to a
-- Repository instead of a LinkedGitlabProject). Concurrency is enforced at
-- the database level: a partial UNIQUE index on (repository_id) WHERE
-- completed_at IS NULL (migration 000019) means CreateRepositorySyncRun
-- itself fails with a unique-constraint violation when a run is already in
-- progress for the same repository, which internal/mrsync maps to
-- ErrRunInProgress (HTTP 409), the same as issue sync's gitlab_sync_runs.

-- name: CreateRepositorySyncRun :one
INSERT INTO repository_sync_runs (repository_id, kind, status, started_at)
VALUES ($1, $2, 'running', now())
RETURNING *;

-- name: CompleteRepositorySyncRun :one
UPDATE repository_sync_runs
SET status = 'succeeded',
    mrs_seen = $2,
    mrs_created = $3,
    mrs_updated = $4,
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: FailRepositorySyncRun :one
UPDATE repository_sync_runs
SET status = 'failed',
    mrs_seen = $2,
    mrs_created = $3,
    mrs_updated = $4,
    error_message = $5,
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: GetRepositorySyncRunByID :one
-- Unscoped, like GetGitlabSyncRunByID: the background worker
-- (internal/mrsync) reads this with no acting user of its own — the job row
-- that names this run's ID was already authorized when it was created
-- (automatically, alongside the repository, at link creation).
SELECT * FROM repository_sync_runs WHERE id = $1;
