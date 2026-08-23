DROP INDEX IF EXISTS idx_backlogs_project_id_assignee_user_id;
DROP INDEX IF EXISTS idx_tasks_project_id_assignee_user_id;
ALTER TABLE backlogs DROP COLUMN IF EXISTS assignee_user_id;
ALTER TABLE tasks    DROP COLUMN IF EXISTS assignee_user_id;
