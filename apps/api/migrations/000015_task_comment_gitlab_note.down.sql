DROP INDEX IF EXISTS idx_task_comments_gitlab_note_id;
ALTER TABLE task_comments DROP COLUMN IF EXISTS gitlab_note_id;
