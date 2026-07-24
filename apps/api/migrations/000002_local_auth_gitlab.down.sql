ALTER TABLE pull_request_reviewers
    DROP CONSTRAINT pull_request_reviewers_pkey;
ALTER TABLE pull_request_reviewers
    RENAME COLUMN gitlab_username TO github_login;
ALTER TABLE pull_request_reviewers
    ADD PRIMARY KEY (pull_request_id, github_login);

ALTER TABLE pull_requests
    RENAME COLUMN gitlab_updated_at TO github_updated_at;
ALTER TABLE pull_requests
    RENAME COLUMN gitlab_created_at TO github_created_at;
ALTER TABLE pull_requests
    RENAME COLUMN author_gitlab_username TO author_github_login;
ALTER TABLE pull_requests
    RENAME COLUMN gitlab_merge_request_id TO github_pull_request_id;

ALTER TABLE repositories
    RENAME COLUMN gitlab_project_id TO github_repository_id;

ALTER TABLE organizations
    RENAME COLUMN gitlab_group_id TO github_organization_id;

ALTER TABLE users
    DROP CONSTRAINT users_email_key,
    DROP CONSTRAINT users_username_key,
    DROP COLUMN password_hash,
    DROP COLUMN email,
    DROP COLUMN username,
    ADD COLUMN encrypted_access_token BYTEA NOT NULL,
    ADD COLUMN avatar_url             TEXT NOT NULL DEFAULT '',
    ADD COLUMN github_login           TEXT NOT NULL DEFAULT '',
    ADD COLUMN github_user_id         BIGINT,
    ADD CONSTRAINT users_github_user_id_key UNIQUE (github_user_id);
