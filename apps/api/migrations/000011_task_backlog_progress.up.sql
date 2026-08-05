-- Progress is the app's own four-stage work state, app-only like priority and
-- a task's startDate (see CLAUDE.md's "Task & backlog scheduling" section):
-- GitLab CE issues have no such field, so it never syncs to GitLab.
--
-- It is deliberately NOT tasks.status. That column is the GitLab issue state
-- (open/closed) and is kept in sync both ways; progress is independent of it,
-- so a task can be 'closed' while still reading 'on_hold' and neither value
-- ever writes the other. TEXT + CHECK follows the same enum convention
-- tasks.status and priority already use. NOT NULL DEFAULT 'not_started' means
-- every task and backlog always has a progress to sort/filter by, with no
-- NULL case to special-case in ORDER BY.
ALTER TABLE tasks ADD COLUMN progress TEXT NOT NULL DEFAULT 'not_started'
    CHECK (progress IN ('not_started', 'in_progress', 'on_hold', 'done'));
ALTER TABLE backlogs ADD COLUMN progress TEXT NOT NULL DEFAULT 'not_started'
    CHECK (progress IN ('not_started', 'in_progress', 'on_hold', 'done'));
