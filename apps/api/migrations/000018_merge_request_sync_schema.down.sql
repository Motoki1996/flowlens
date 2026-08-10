ALTER TABLE merge_requests
    DROP COLUMN first_reviewed_at,
    DROP COLUMN pipeline_status,
    DROP COLUMN pipeline_id,
    DROP COLUMN pipeline_updated_at;

ALTER TABLE merge_request_reviewers RENAME COLUMN merge_request_id TO pull_request_id;

ALTER INDEX idx_merge_requests_repository_id RENAME TO idx_pull_requests_repository_id;
ALTER INDEX idx_merge_requests_state RENAME TO idx_pull_requests_state;
ALTER INDEX idx_repository_sync_runs_repository_id RENAME TO idx_sync_runs_repository_id;

ALTER TABLE merge_requests RENAME TO pull_requests;
ALTER TABLE merge_request_reviewers RENAME TO pull_request_reviewers;
ALTER TABLE repository_sync_runs RENAME TO sync_runs;

ALTER TABLE repositories
    DROP CONSTRAINT repositories_linked_gitlab_project_id_key,
    DROP COLUMN linked_gitlab_project_id,
    ADD COLUMN gitlab_project_id BIGINT,
    ADD COLUMN organization_id UUID;

CREATE TABLE organizations (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gitlab_group_id        BIGINT      NOT NULL UNIQUE,
    login                  TEXT        NOT NULL,
    display_name           TEXT        NOT NULL DEFAULT '',
    avatar_url             TEXT        NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organization_members (
    organization_id UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            TEXT        NOT NULL DEFAULT 'member',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);

ALTER TABLE repositories
    ALTER COLUMN organization_id SET NOT NULL,
    ADD CONSTRAINT repositories_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE,
    ALTER COLUMN gitlab_project_id SET NOT NULL,
    ADD CONSTRAINT repositories_gitlab_project_id_key UNIQUE (gitlab_project_id);

CREATE INDEX idx_repositories_organization_id ON repositories(organization_id);
