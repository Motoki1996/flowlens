-- Task management with GitLab CE issue sync (docs/plans/issue-sync.md).
-- Existing repositories / pull_requests / sync_runs tables (MR visualisation
-- feature) are left untouched.

CREATE TABLE projects (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id, name)
);

CREATE TABLE backlogs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    position    INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_backlogs_project_id ON backlogs(project_id);

CREATE TABLE tasks (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id               UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- NULL = 未分類 (unfiled); a backlog being deleted must not delete its tasks.
    backlog_id               UUID        REFERENCES backlogs(id) ON DELETE SET NULL,
    title                    TEXT        NOT NULL,
    description              TEXT        NOT NULL DEFAULT '',
    status                   TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    closed_at                TIMESTAMPTZ,
    assignee_gitlab_user_id  BIGINT,
    assignee_gitlab_username TEXT        NOT NULL DEFAULT '',
    labels                   TEXT[]      NOT NULL DEFAULT '{}',
    due_on                   DATE,
    position                 INTEGER     NOT NULL DEFAULT 0,
    created_by_user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tasks_project_id_backlog_id ON tasks(project_id, backlog_id);
CREATE INDEX idx_tasks_project_id_status ON tasks(project_id, status);

CREATE TABLE task_ai_contexts (
    task_id             UUID PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    acceptance_criteria TEXT        NOT NULL DEFAULT '',
    ai_context          TEXT        NOT NULL DEFAULT '',
    allowed_scope       TEXT        NOT NULL DEFAULT '',
    forbidden_scope     TEXT        NOT NULL DEFAULT '',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gitlab_connections (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id             UUID        NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    base_url               TEXT        NOT NULL,
    -- AES-256-GCM ciphertext; see internal/crypto.
    encrypted_token        BYTEA       NOT NULL,
    token_gitlab_user_id   BIGINT,
    token_gitlab_username  TEXT        NOT NULL DEFAULT '',
    last_verified_at       TIMESTAMPTZ,
    last_verify_error      TEXT        NOT NULL DEFAULT '',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE linked_gitlab_projects (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gitlab_connection_id    UUID        NOT NULL REFERENCES gitlab_connections(id) ON DELETE CASCADE,
    gitlab_project_id       BIGINT      NOT NULL,
    path_with_namespace     TEXT        NOT NULL,
    name                    TEXT        NOT NULL,
    web_url                 TEXT        NOT NULL DEFAULT '',
    sync_scope              TEXT        NOT NULL DEFAULT 'all' CHECK (sync_scope IN ('all', 'labels')),
    sync_labels             TEXT[]      NOT NULL DEFAULT '{}',
    webhook_id              BIGINT,
    encrypted_webhook_secret BYTEA,
    webhook_registered_at   TIMESTAMPTZ,
    initial_import_status   TEXT        NOT NULL DEFAULT 'pending',
    last_synced_at          TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (gitlab_connection_id, gitlab_project_id)
);

CREATE INDEX idx_linked_gitlab_projects_gitlab_connection_id ON linked_gitlab_projects(gitlab_connection_id);

CREATE TABLE task_gitlab_links (
    task_id                    UUID PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    linked_gitlab_project_id  UUID        NOT NULL REFERENCES linked_gitlab_projects(id) ON DELETE CASCADE,
    gitlab_issue_id            BIGINT      NOT NULL,
    gitlab_issue_iid           BIGINT      NOT NULL,
    gitlab_web_url             TEXT        NOT NULL DEFAULT '',
    -- Newest GitLab-side updated_at we have applied.
    gitlab_updated_at          TIMESTAMPTZ,
    -- Hash of what we last pushed, to detect our own webhook echo.
    last_pushed_fingerprint    TEXT        NOT NULL DEFAULT '',
    sync_status                TEXT        NOT NULL DEFAULT 'pending' CHECK (sync_status IN ('synced', 'pending', 'failed')),
    last_error                 TEXT        NOT NULL DEFAULT '',
    last_synced_at             TIMESTAMPTZ,
    -- The 1:1 task <-> issue guarantee.
    UNIQUE (linked_gitlab_project_id, gitlab_issue_iid)
);

CREATE INDEX idx_task_gitlab_links_linked_gitlab_project_id ON task_gitlab_links(linked_gitlab_project_id);

CREATE TABLE gitlab_sync_runs (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    linked_gitlab_project_id UUID        NOT NULL REFERENCES linked_gitlab_projects(id) ON DELETE CASCADE,
    kind                      TEXT        NOT NULL CHECK (kind IN ('initial_import', 'manual_resync')),
    status                    TEXT        NOT NULL,
    issues_seen               INTEGER     NOT NULL DEFAULT 0,
    issues_created            INTEGER     NOT NULL DEFAULT 0,
    issues_updated            INTEGER     NOT NULL DEFAULT 0,
    started_at                TIMESTAMPTZ,
    completed_at              TIMESTAMPTZ,
    error_message             TEXT        NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_gitlab_sync_runs_linked_gitlab_project_id ON gitlab_sync_runs(linked_gitlab_project_id);

CREATE TABLE webhook_events (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    linked_gitlab_project_id UUID        NOT NULL REFERENCES linked_gitlab_projects(id) ON DELETE CASCADE,
    delivery_uuid             TEXT        NOT NULL,
    event_name                TEXT        NOT NULL DEFAULT '',
    object_kind                TEXT        NOT NULL DEFAULT '',
    gitlab_issue_iid          BIGINT,
    payload                    JSONB       NOT NULL,
    gitlab_updated_at         TIMESTAMPTZ,
    status                     TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processed', 'skipped', 'failed')),
    skip_reason                TEXT        NOT NULL DEFAULT '',
    error_message              TEXT        NOT NULL DEFAULT '',
    received_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at                TIMESTAMPTZ,
    -- Duplicate webhook deliveries must be a no-op.
    UNIQUE (linked_gitlab_project_id, delivery_uuid)
);

CREATE INDEX idx_webhook_events_linked_gitlab_project_id ON webhook_events(linked_gitlab_project_id);
CREATE INDEX idx_webhook_events_status ON webhook_events(status);

CREATE TABLE sync_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task_id       UUID        REFERENCES tasks(id) ON DELETE CASCADE,
    kind          TEXT        NOT NULL,
    payload       JSONB       NOT NULL,
    -- Collapses rapid repeated edits into one pending job (e.g. "issue.update:<task_id>").
    dedupe_key    TEXT        UNIQUE,
    status        TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    attempts      INTEGER     NOT NULL DEFAULT 0,
    run_after     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error    TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sync_jobs_status_run_after ON sync_jobs(status, run_after);
CREATE INDEX idx_sync_jobs_project_id ON sync_jobs(project_id);

CREATE TABLE project_api_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    -- SHA-256 hash of the opaque bearer token, like sessions.token_hash.
    token_hash    TEXT        NOT NULL UNIQUE,
    last_used_at  TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_api_tokens_project_id ON project_api_tokens(project_id);
