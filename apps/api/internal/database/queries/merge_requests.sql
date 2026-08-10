-- merge_requests mirrors a repository's GitLab merge requests, read-only
-- (issue #111: FlowLens never writes back to GitLab). Idempotency comes
-- from the UNIQUE constraint on gitlab_merge_request_id, the same shape as
-- task_gitlab_links' issue-iid uniqueness.

-- name: CreateMergeRequest :one
INSERT INTO merge_requests (
    repository_id, gitlab_merge_request_id, number, title, state, is_draft,
    author_gitlab_username, author_avatar_url, base_branch, head_branch,
    gitlab_created_at, gitlab_updated_at, merged_at, closed_at, html_url,
    pipeline_status, pipeline_id, pipeline_updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
)
RETURNING *;

-- name: GetMergeRequestByGitlabMergeRequestID :one
SELECT * FROM merge_requests WHERE gitlab_merge_request_id = $1;

-- GetMergeRequestByRepositoryAndNumber looks up a merge request by its
-- project-scoped IID (the "number" column) rather than its global GitLab
-- id — the key a "Pipeline Hook" delivery's merge_request.iid gives
-- (internal/webhookapply), unlike an MR event's own object_attributes.id.

-- name: GetMergeRequestByRepositoryAndNumber :one
SELECT * FROM merge_requests WHERE repository_id = $1 AND number = $2;

-- UpdateMergeRequest applies the latest GitLab-sourced fields to an
-- already-imported merge request. first_reviewed_at and task_id are
-- intentionally not touched here — see UpdateMergeRequestFirstReviewedAt
-- and UpdateMergeRequestTaskID, which set them at most once each.

-- name: UpdateMergeRequest :one
UPDATE merge_requests
SET title = $2,
    state = $3,
    is_draft = $4,
    author_gitlab_username = $5,
    author_avatar_url = $6,
    base_branch = $7,
    head_branch = $8,
    gitlab_updated_at = $9,
    merged_at = $10,
    closed_at = $11,
    html_url = $12,
    pipeline_status = $13,
    pipeline_id = $14,
    pipeline_updated_at = $15,
    updated_at = now()
WHERE gitlab_merge_request_id = $1
RETURNING *;

-- UpdateMergeRequestPipeline applies a "Pipeline Hook" delivery's status to
-- the merge request it belongs to (internal/webhookapply), narrower than
-- UpdateMergeRequest since a pipeline event carries no MR fields at all.

-- name: UpdateMergeRequestPipeline :one
UPDATE merge_requests
SET pipeline_status = $2,
    pipeline_id = $3,
    pipeline_updated_at = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- UpdateMergeRequestFirstReviewedAt records the first review activity
-- (ADR-0011 §3) at most once: the WHERE guard means a later, later-timed
-- call is a no-op rather than clobbering the true first review.

-- name: UpdateMergeRequestFirstReviewedAt :one
UPDATE merge_requests
SET first_reviewed_at = $2
WHERE id = $1 AND first_reviewed_at IS NULL
RETURNING *;

-- UpdateMergeRequestTaskID links a merge request to the task its
-- description/branch name references (resolved through task_gitlab_links).

-- name: UpdateMergeRequestTaskID :one
UPDATE merge_requests
SET task_id = $2
WHERE id = $1
RETURNING *;
