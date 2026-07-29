-- gitlab_sync_runs records one project.import / project.resync execution
-- against a linked GitLab project (docs/plans/issue-sync.md's SyncRun
-- object, issue #25). Concurrency is enforced at the database level, not by
-- a check-then-insert race in Go: a partial UNIQUE index on
-- (linked_gitlab_project_id) WHERE completed_at IS NULL (migration 000006)
-- means CreateGitlabSyncRun itself fails with a unique-constraint violation
-- when a run is already in progress for the same link, which
-- internal/projectsync maps to ErrRunInProgress (HTTP 409).

-- name: CreateGitlabSyncRun :one
INSERT INTO gitlab_sync_runs (linked_gitlab_project_id, kind, status, started_at)
VALUES ($1, $2, 'running', now())
RETURNING *;

-- name: CompleteGitlabSyncRun :one
UPDATE gitlab_sync_runs
SET status = 'succeeded',
    issues_seen = $2,
    issues_created = $3,
    issues_updated = $4,
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: FailGitlabSyncRun :one
UPDATE gitlab_sync_runs
SET status = 'failed',
    issues_seen = $2,
    issues_created = $3,
    issues_updated = $4,
    error_message = $5,
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: GetGitlabSyncRunByID :one
-- Unscoped, like GetLinkedGitlabProjectByID: the background worker
-- (internal/projectsync) reads this with no acting user of its own — the
-- job row that names this run's ID was already authorized when it was
-- created (either automatically at link creation, or by an owner-scoped
-- HTTP request).
SELECT * FROM gitlab_sync_runs WHERE id = $1;

-- name: ListGitlabSyncRunsByLinkedGitlabProjectID :many
-- Ownership of linkID must already be verified by the caller via
-- GetLinkedGitlabProjectForOwner before this runs.
SELECT * FROM gitlab_sync_runs
WHERE linked_gitlab_project_id = $1
ORDER BY created_at DESC;
