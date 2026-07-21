-- FlowLens initial schema.
-- gen_random_uuid() is built into PostgreSQL 13+.

CREATE TABLE users (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_user_id           BIGINT      NOT NULL UNIQUE,
    github_login             TEXT        NOT NULL,
    display_name             TEXT        NOT NULL DEFAULT '',
    avatar_url               TEXT        NOT NULL DEFAULT '',
    encrypted_access_token   BYTEA       NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 hash of the opaque session token carried in the cookie.
    token_hash   TEXT        NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- ---------------------------------------------------------------------
-- The tables below are part of the MVP data model and are created now so
-- the database foundation is complete and extensible. Application logic
-- for organizations / repositories / pull requests lands in later phases.
-- ---------------------------------------------------------------------

CREATE TABLE organizations (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_organization_id BIGINT      NOT NULL UNIQUE,
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

CREATE TABLE repositories (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id      UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    github_repository_id BIGINT      NOT NULL UNIQUE,
    name                 TEXT        NOT NULL,
    full_name            TEXT        NOT NULL,
    description          TEXT        NOT NULL DEFAULT '',
    is_private           BOOLEAN     NOT NULL DEFAULT false,
    default_branch       TEXT        NOT NULL DEFAULT 'main',
    html_url             TEXT        NOT NULL DEFAULT '',
    is_active            BOOLEAN     NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_repositories_organization_id ON repositories(organization_id);

CREATE TABLE pull_requests (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id          UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    github_pull_request_id BIGINT      NOT NULL UNIQUE,
    number                 INTEGER     NOT NULL,
    title                  TEXT        NOT NULL DEFAULT '',
    state                  TEXT        NOT NULL,
    is_draft               BOOLEAN     NOT NULL DEFAULT false,
    author_github_login    TEXT        NOT NULL DEFAULT '',
    author_avatar_url      TEXT        NOT NULL DEFAULT '',
    base_branch            TEXT        NOT NULL DEFAULT '',
    head_branch            TEXT        NOT NULL DEFAULT '',
    additions              INTEGER     NOT NULL DEFAULT 0,
    deletions              INTEGER     NOT NULL DEFAULT 0,
    changed_files          INTEGER     NOT NULL DEFAULT 0,
    github_created_at       TIMESTAMPTZ,
    github_updated_at       TIMESTAMPTZ,
    merged_at               TIMESTAMPTZ,
    closed_at               TIMESTAMPTZ,
    html_url               TEXT        NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repository_id, number)
);

CREATE INDEX idx_pull_requests_repository_id ON pull_requests(repository_id);
CREATE INDEX idx_pull_requests_state ON pull_requests(state);

CREATE TABLE pull_request_reviewers (
    pull_request_id UUID        NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    github_login    TEXT        NOT NULL,
    avatar_url      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (pull_request_id, github_login)
);

CREATE TABLE sync_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID        NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    status        TEXT        NOT NULL,
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    error_message TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sync_runs_repository_id ON sync_runs(repository_id);
