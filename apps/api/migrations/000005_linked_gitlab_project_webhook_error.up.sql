-- Webhook registration can fail (most commonly: the connection's personal
-- access token belongs to a user below Maintainer on the GitLab project).
-- The link is kept either way (docs/plans/issue-sync.md); this column lets
-- the reason surface in the API response instead of just leaving
-- webhook_registered_at unset with no explanation.
ALTER TABLE linked_gitlab_projects ADD COLUMN webhook_registration_error TEXT NOT NULL DEFAULT '';
