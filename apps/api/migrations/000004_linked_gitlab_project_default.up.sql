-- A linked GitLab project can be marked "default": the destination for a
-- newly created task's issue when a project has more than one linked GitLab
-- project (docs/plans/issue-sync.md). The first project linked becomes
-- default automatically; it stays changeable afterwards.
ALTER TABLE linked_gitlab_projects ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT false;

-- At most one default per GitLab connection.
CREATE UNIQUE INDEX idx_linked_gitlab_projects_default
    ON linked_gitlab_projects (gitlab_connection_id)
    WHERE is_default;
