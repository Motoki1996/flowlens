-- Switch login from GitHub OAuth to local username/password auth, and
-- retarget the data-source columns from GitHub to GitLab CE.

ALTER TABLE users
    DROP COLUMN github_user_id,
    DROP COLUMN github_login,
    DROP COLUMN avatar_url,
    DROP COLUMN encrypted_access_token,
    ADD COLUMN username      TEXT NOT NULL,
    ADD COLUMN email         TEXT NOT NULL,
    ADD COLUMN password_hash TEXT NOT NULL,
    ADD CONSTRAINT users_username_key UNIQUE (username),
    ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE organizations
    RENAME COLUMN github_organization_id TO gitlab_group_id;

ALTER TABLE repositories
    RENAME COLUMN github_repository_id TO gitlab_project_id;

ALTER TABLE pull_requests
    RENAME COLUMN github_pull_request_id TO gitlab_merge_request_id;
ALTER TABLE pull_requests
    RENAME COLUMN author_github_login TO author_gitlab_username;
ALTER TABLE pull_requests
    RENAME COLUMN github_created_at TO gitlab_created_at;
ALTER TABLE pull_requests
    RENAME COLUMN github_updated_at TO gitlab_updated_at;

ALTER TABLE pull_request_reviewers
    DROP CONSTRAINT pull_request_reviewers_pkey;
ALTER TABLE pull_request_reviewers
    RENAME COLUMN github_login TO gitlab_username;
ALTER TABLE pull_request_reviewers
    ADD PRIMARY KEY (pull_request_id, gitlab_username);
