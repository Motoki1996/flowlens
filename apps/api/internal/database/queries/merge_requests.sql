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
-- gitlab_updated_at is COALESCEd rather than written unconditionally: a
-- delivery whose updated_at didn't parse (issue #183) passes NULL, and that
-- must never erase an already-recorded baseline (see the same reasoning on
-- MarkTaskGitlabLinkAppliedForTask in task_gitlab_links.sql).

-- name: UpdateMergeRequest :one
UPDATE merge_requests
SET title = $2,
    state = $3,
    is_draft = $4,
    author_gitlab_username = $5,
    author_avatar_url = $6,
    base_branch = $7,
    head_branch = $8,
    gitlab_updated_at = COALESCE($9, gitlab_updated_at),
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

-- ListMergeRequestsByProject backs the merge-request collection view (issue
-- #112), scoped through repositories -> linked_gitlab_projects ->
-- gitlab_connections to the app project the caller is a member of, the same
-- project_members EXISTS check GetTaskForOwner uses. state/author/task_id
-- follow ListTasksByProject's "empty/NULL disables it" convention;
-- since/until bound gitlab_created_at. sort_by_updated switches the primary
-- order from gitlab_created_at to gitlab_updated_at, both DESC with created_at
-- as the tiebreak, so a merge request with no GitLab timestamp yet still
-- sorts deterministically.
--
-- LIMIT/OFFSET paging follows the same "fetch one extra row to detect a next
-- page" convention ListWebhookEventsByLinkedGitlabProjectID/internal/webhookevent
-- established: a long-lived repository accumulates thousands of merged merge
-- requests, and this view used to return every one of them in a single
-- response. idx_merge_requests_repo_state_{created,updated} (migration 28)
-- back both orderings.

-- name: ListMergeRequestsByProject :many
SELECT mr.id, mr.repository_id, mr.gitlab_merge_request_id, mr.number, mr.title, mr.state, mr.is_draft, mr.author_gitlab_username, mr.author_avatar_url, mr.base_branch, mr.head_branch, mr.additions, mr.deletions, mr.changed_files, mr.gitlab_created_at, mr.gitlab_updated_at, mr.merged_at, mr.closed_at, mr.html_url, mr.created_at, mr.updated_at, mr.first_reviewed_at, mr.pipeline_status, mr.pipeline_id, mr.pipeline_updated_at, mr.task_id
FROM merge_requests mr
JOIN repositories r ON r.id = mr.repository_id
JOIN linked_gitlab_projects lgp ON lgp.id = r.linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE gc.project_id = sqlc.arg(project_id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND (sqlc.arg(state)::text = '' OR mr.state = sqlc.arg(state))
  AND (sqlc.arg(author)::text = '' OR mr.author_gitlab_username = sqlc.arg(author))
  AND (sqlc.narg(task_id)::uuid IS NULL OR mr.task_id = sqlc.narg(task_id))
  AND (sqlc.narg(since)::timestamptz IS NULL OR mr.gitlab_created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR mr.gitlab_created_at <= sqlc.narg(until))
ORDER BY
  (CASE WHEN sqlc.arg(sort_by_updated)::boolean THEN mr.gitlab_updated_at ELSE mr.gitlab_created_at END) DESC NULLS LAST,
  mr.created_at DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);

-- CountMergeRequestsByProject is ListMergeRequestsByProject's total, with
-- the same scoping and the same filters, minus the ordering and paging. The
-- collection view needs it because the paged list can no longer be counted
-- client-side by its length, and the project sidebar's merge-request badge
-- is a count rather than a list — it used to fetch every row just to read
-- .length off it.

-- name: CountMergeRequestsByProject :one
SELECT count(*)
FROM merge_requests mr
JOIN repositories r ON r.id = mr.repository_id
JOIN linked_gitlab_projects lgp ON lgp.id = r.linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE gc.project_id = sqlc.arg(project_id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND (sqlc.arg(state)::text = '' OR mr.state = sqlc.arg(state))
  AND (sqlc.arg(author)::text = '' OR mr.author_gitlab_username = sqlc.arg(author))
  AND (sqlc.narg(task_id)::uuid IS NULL OR mr.task_id = sqlc.narg(task_id))
  AND (sqlc.narg(since)::timestamptz IS NULL OR mr.gitlab_created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR mr.gitlab_created_at <= sqlc.narg(until));

-- GetMergeRequestForOwner is ListMergeRequestsByProject's single-object
-- counterpart, backing the merge-request single view. Scoped the same way,
-- so a merge request belonging to a project the caller isn't a member of is
-- indistinguishable from one that doesn't exist.

-- name: GetMergeRequestForOwner :one
SELECT mr.id, mr.repository_id, mr.gitlab_merge_request_id, mr.number, mr.title, mr.state, mr.is_draft, mr.author_gitlab_username, mr.author_avatar_url, mr.base_branch, mr.head_branch, mr.additions, mr.deletions, mr.changed_files, mr.gitlab_created_at, mr.gitlab_updated_at, mr.merged_at, mr.closed_at, mr.html_url, mr.created_at, mr.updated_at, mr.first_reviewed_at, mr.pipeline_status, mr.pipeline_id, mr.pipeline_updated_at, mr.task_id
FROM merge_requests mr
JOIN repositories r ON r.id = mr.repository_id
JOIN linked_gitlab_projects lgp ON lgp.id = r.linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE mr.id = sqlc.arg(id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  );

-- ListMergeRequestsForMetrics backs the delivery-metrics aggregation (issue
-- #113): the narrow column set deliverymetrics.Service needs to compute
-- median/p90 durations, size distribution, pipeline success rate and
-- throughput in the application layer, following docs/testing.md's "test
-- aggregation/derivation logic at the domain layer with fakes" guidance.
-- Scoped and filtered exactly like ListMergeRequestsByProject (same
-- project_members check, same since/until bounding gitlab_created_at), minus
-- the state/author/task_id/sort filters that view alone needs.

-- name: ListMergeRequestsForMetrics :many
SELECT mr.state, mr.additions, mr.deletions, mr.changed_files, mr.gitlab_created_at, mr.merged_at, mr.first_reviewed_at, mr.pipeline_status
FROM merge_requests mr
JOIN repositories r ON r.id = mr.repository_id
JOIN linked_gitlab_projects lgp ON lgp.id = r.linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE gc.project_id = sqlc.arg(project_id)
  AND EXISTS (
    SELECT 1 FROM project_members pm
    WHERE pm.project_id = gc.project_id AND pm.user_id = sqlc.arg(owner_user_id)
  )
  AND (sqlc.narg(since)::timestamptz IS NULL OR mr.gitlab_created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR mr.gitlab_created_at <= sqlc.narg(until));

-- GetMergeRequestProjectID is the lightweight, unscoped lookup
-- requireTokenResourceProject (internal/http, issue #66) uses to enforce a
-- bearer token's project boundary on a single-merge-request URL, the same
-- role GetTaskProjectID plays for tasks.

-- name: GetMergeRequestProjectID :one
SELECT gc.project_id
FROM merge_requests mr
JOIN repositories r ON r.id = mr.repository_id
JOIN linked_gitlab_projects lgp ON lgp.id = r.linked_gitlab_project_id
JOIN gitlab_connections gc ON gc.id = lgp.gitlab_connection_id
WHERE mr.id = sqlc.arg(id);
