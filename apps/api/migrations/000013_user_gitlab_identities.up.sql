CREATE TABLE user_gitlab_identities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    gitlab_base_url TEXT        NOT NULL,
    gitlab_user_id  BIGINT      NOT NULL,
    gitlab_username TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, gitlab_base_url)
);
