DROP INDEX IF EXISTS idx_linked_gitlab_projects_default;
ALTER TABLE linked_gitlab_projects DROP COLUMN IF EXISTS is_default;
