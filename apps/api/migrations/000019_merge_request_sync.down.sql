DROP INDEX idx_merge_requests_task_id;
ALTER TABLE merge_requests DROP COLUMN task_id;

DROP INDEX idx_repository_sync_runs_one_running_per_repository;

ALTER TABLE repository_sync_runs
    DROP COLUMN kind,
    DROP COLUMN mrs_seen,
    DROP COLUMN mrs_created,
    DROP COLUMN mrs_updated;
