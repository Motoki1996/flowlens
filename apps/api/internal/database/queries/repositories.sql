-- repositories is the MR-tracking sibling of a linked GitLab project
-- (ADR-0011 §1): one linked_gitlab_projects row has at most one repositories
-- row, created automatically alongside it (internal/linkedproject.Service.Create),
-- never through a separate flow. No owner column of its own — every access
-- goes through linked_gitlab_projects, same as task_gitlab_links.

-- name: CreateRepository :one
INSERT INTO repositories (
    linked_gitlab_project_id, name, full_name, description, is_private,
    default_branch, html_url, is_active
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, true
)
RETURNING *;

-- name: GetRepositoryByLinkedGitlabProjectID :one
-- Unscoped, like GetLinkedGitlabProjectByID: the background sync worker
-- (internal/mrsync) reads this with no acting user of its own — the job row
-- that names this repository's linked project was already authorized when
-- it was created.
SELECT * FROM repositories WHERE linked_gitlab_project_id = $1;

-- name: GetRepositoryByID :one
SELECT * FROM repositories WHERE id = $1;
