-- internal/gitlabidentity used to store gitlab_base_url trimmed only, while
-- internal/gitlabconn's connection base_url is normalized (trailing slash
-- stripped). ?assignee=me joins the two on equality, so a trailing slash
-- typed into "GitLab identity" (e.g. "https://gitlab.example.com/") never
-- matched a connection saved as "https://gitlab.example.com" and silently
-- zeroed out the filter. Both now normalize the same way (internal/gitlaburl)
-- going forward; this backfills rows written before that fix.
--
-- Skips a row whose normalized form would collide with another identity
-- already registered for the same user, rather than violating the
-- (user_id, gitlab_base_url) unique constraint — that would mean the user
-- had separately registered both "https://x.com" and "https://x.com/",
-- which the API never allowed as a matching pair anyway.
UPDATE user_gitlab_identities u1
SET gitlab_base_url = regexp_replace(u1.gitlab_base_url, '/+$', '')
WHERE u1.gitlab_base_url ~ '/$'
  AND NOT EXISTS (
    SELECT 1 FROM user_gitlab_identities u2
    WHERE u2.user_id = u1.user_id
      AND u2.id <> u1.id
      AND u2.gitlab_base_url = regexp_replace(u1.gitlab_base_url, '/+$', '')
  );
