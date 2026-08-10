-- Schema alignment for merge-request sync (issue #110 / ADR-0011). Design and
-- schema only — the sync engine itself is issue #111. No app behaviour
-- changes: these tables are still unpopulated.

-- repositories becomes the MR-tracking sibling of a linked GitLab project:
-- one linked_gitlab_projects row has at most one repositories row.
ALTER TABLE repositories
    DROP COLUMN organization_id,
    DROP COLUMN gitlab_project_id,
    ADD COLUMN linked_gitlab_project_id UUID NOT NULL UNIQUE
        REFERENCES linked_gitlab_projects(id) ON DELETE CASCADE;

-- organizations / organization_members modeled a GitHub-org concept that has
-- no place now that repositories hangs off linked_gitlab_projects (a GitLab
-- project under a per-FlowLens-project connection, per ADR-0008) instead of
-- an org/group. Nothing references them now that repositories is repointed.
DROP TABLE organization_members;
DROP TABLE organizations;

-- Table renames to GitLab vocabulary, done now while every table below is
-- still empty (see ADR-0011 §4).
ALTER TABLE pull_requests RENAME TO merge_requests;
ALTER TABLE pull_request_reviewers RENAME TO merge_request_reviewers;
ALTER TABLE sync_runs RENAME TO repository_sync_runs;

ALTER INDEX idx_pull_requests_repository_id RENAME TO idx_merge_requests_repository_id;
ALTER INDEX idx_pull_requests_state RENAME TO idx_merge_requests_state;
ALTER INDEX idx_sync_runs_repository_id RENAME TO idx_repository_sync_runs_repository_id;

ALTER TABLE merge_request_reviewers RENAME COLUMN pull_request_id TO merge_request_id;

-- Minimal delivery-flow metrics (ADR-0011 §3): open->first review is
-- gitlab_created_at -> first_reviewed_at, first review->merge is
-- first_reviewed_at -> merged_at (both already present). Pipeline state
-- tracks the MR's latest pipeline only, not history.
ALTER TABLE merge_requests
    ADD COLUMN first_reviewed_at   TIMESTAMPTZ,
    ADD COLUMN pipeline_status     TEXT NOT NULL DEFAULT '',
    ADD COLUMN pipeline_id         BIGINT,
    ADD COLUMN pipeline_updated_at TIMESTAMPTZ;
