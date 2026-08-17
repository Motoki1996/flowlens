-- A backlog can name its own destination for newly created issues, overriding
-- the project-wide default link (000004's linked_gitlab_projects.is_default).
-- internal/task.Service.Create resolves the destination in this order: the
-- task's backlog's link, then the project's default link, then nothing at all
-- (a purely local task).
--
-- NULL — the default, and the only value a backlog created before this
-- migration can have — means "use the project's default link", so nothing
-- about an existing project's behaviour changes.
--
-- ON DELETE SET NULL rather than CASCADE: unlinking a GitLab project must not
-- delete the backlog, it must fall the backlog back to the project default.
--
-- Nothing constrains the referenced link to a project's *own* GitLab
-- connection at the schema level — that check spans backlogs -> projects <-
-- gitlab_connections <- linked_gitlab_projects and lives in internal/backlog,
-- alongside the other cross-table rules (e.g. internal/taskdependency's cycle
-- check).
ALTER TABLE backlogs ADD COLUMN default_linked_gitlab_project_id UUID
    REFERENCES linked_gitlab_projects(id) ON DELETE SET NULL;

-- Supports the ON DELETE SET NULL scan when a linked project is removed.
CREATE INDEX idx_backlogs_default_linked_gitlab_project
    ON backlogs(default_linked_gitlab_project_id)
    WHERE default_linked_gitlab_project_id IS NOT NULL;
