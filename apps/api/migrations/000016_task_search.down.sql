DROP INDEX IF EXISTS idx_tasks_search_vector;
ALTER TABLE tasks DROP COLUMN IF EXISTS search_vector;
