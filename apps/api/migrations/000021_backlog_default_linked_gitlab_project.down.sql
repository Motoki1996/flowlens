DROP INDEX IF EXISTS idx_backlogs_default_linked_gitlab_project;
ALTER TABLE backlogs DROP COLUMN IF EXISTS default_linked_gitlab_project_id;
