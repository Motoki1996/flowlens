DROP INDEX IF EXISTS idx_tasks_project_id_epic_id;
ALTER TABLE tasks DROP COLUMN IF EXISTS epic_id;
DROP TABLE IF EXISTS epics;
