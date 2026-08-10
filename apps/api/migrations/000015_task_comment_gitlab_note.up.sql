-- Bidirectional GitLab issue-notes sync (#104). A task can have many
-- comments, so the per-task last_pushed_fingerprint echo guard used for
-- issue title/description/labels (task_gitlab_links.last_pushed_fingerprint)
-- doesn't generalize here. Instead each pushed comment's returned GitLab
-- note id is stored on its own task_comments row, so an inbound "Note Hook"
-- webhook carrying the same note id is recognised as FlowLens's own echo.
ALTER TABLE task_comments ADD COLUMN gitlab_note_id BIGINT;

CREATE UNIQUE INDEX idx_task_comments_gitlab_note_id ON task_comments(gitlab_note_id) WHERE gitlab_note_id IS NOT NULL;
