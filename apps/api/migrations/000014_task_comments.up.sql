-- A task's activity log (issue #103): the return path for an AI agent that
-- has been reading task_ai_contexts but had no way to report back what it
-- did or where it got stuck. GitLab-discussion sync is a separate,
-- follow-up feature (#104) — author_kind already allows 'gitlab' so that
-- feature can insert rows here without a schema change of its own.
CREATE TABLE task_comments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id           UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    -- A human's post. NULL for a token's post.
    author_user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    -- A project API token's post. NULL for a human's post.
    author_token_id   UUID REFERENCES project_api_tokens(id) ON DELETE SET NULL,
    author_kind       TEXT NOT NULL CHECK (author_kind IN ('user','agent','gitlab')),
    body              TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_comments_task_id ON task_comments(task_id);
