-- Merge-request/pipeline sync engine (issue #111, building on the
-- design/schema-only ADR-0011 / migration 000018).

-- repository_sync_runs gains the same kind/counts shape gitlab_sync_runs
-- already has for issue sync (ADR-0011 §2: "reusing the
-- gitlab_sync_runs/manual-resync shape, scoped to the Repository").
ALTER TABLE repository_sync_runs
    ADD COLUMN kind        TEXT    NOT NULL DEFAULT 'initial_import'
        CHECK (kind IN ('initial_import', 'manual_resync')),
    ADD COLUMN mrs_seen    INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN mrs_created INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN mrs_updated INTEGER NOT NULL DEFAULT 0;

ALTER TABLE repository_sync_runs ALTER COLUMN kind DROP DEFAULT;

-- One running (uncompleted) run per repository at a time, same guard
-- idx_gitlab_sync_runs_one_running_per_link already gives issue sync.
CREATE UNIQUE INDEX idx_repository_sync_runs_one_running_per_repository
    ON repository_sync_runs (repository_id)
    WHERE completed_at IS NULL;

-- Task <-> MR linkage: parsed from the MR description/branch name (an
-- issue iid reference resolved through task_gitlab_links), not its own
-- link table — a merge request has at most one task, so a column here is
-- enough (issue #111).
ALTER TABLE merge_requests
    ADD COLUMN task_id UUID REFERENCES tasks(id) ON DELETE SET NULL;

CREATE INDEX idx_merge_requests_task_id ON merge_requests(task_id);
