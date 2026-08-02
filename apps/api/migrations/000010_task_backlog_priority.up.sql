-- Priority is app-only, like a task's startDate, task dependencies and a
-- backlog's own dates (see CLAUDE.md's "Task & backlog scheduling" section):
-- GitLab CE issues have no native priority field (priority label / weight is
-- an EE feature), so this never syncs to GitLab. TEXT + CHECK follows the
-- same enum convention tasks.status already uses. NOT NULL DEFAULT 'medium'
-- means every task and backlog always has a priority to sort/filter by, with
-- no NULL case to special-case in ORDER BY.
ALTER TABLE tasks ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'
    CHECK (priority IN ('low', 'medium', 'high', 'urgent'));
ALTER TABLE backlogs ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium'
    CHECK (priority IN ('low', 'medium', 'high', 'urgent'));
