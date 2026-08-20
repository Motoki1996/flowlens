ALTER TABLE task_ai_contexts ADD COLUMN allowed_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE task_ai_contexts ADD COLUMN forbidden_scope TEXT NOT NULL DEFAULT '';

ALTER TABLE backlogs DROP COLUMN IF EXISTS allowed_scope;
ALTER TABLE backlogs DROP COLUMN IF EXISTS forbidden_scope;
