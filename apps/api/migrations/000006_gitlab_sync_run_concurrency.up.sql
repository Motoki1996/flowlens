-- One running (uncompleted) gitlab_sync_runs row per linked GitLab project
-- at a time. CreateGitlabSyncRun relies on this: starting a
-- project.import/project.resync run while one is already in progress hits
-- this unique-constraint violation, which internal/projectsync maps to a
-- 409 Conflict (docs/plans/issue-sync.md's SyncRun object).
CREATE UNIQUE INDEX idx_gitlab_sync_runs_one_running_per_link
    ON gitlab_sync_runs (linked_gitlab_project_id)
    WHERE completed_at IS NULL;
