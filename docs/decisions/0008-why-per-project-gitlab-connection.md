# 0008. Why the GitLab connection is per app project, and tasks link 1:1 to issues

- **Status:** Accepted
- **Date:** 2026-07-25

## Context

`CLAUDE.md` anticipated a **per-user** GitLab personal access token: log in to
FlowLens, connect your own GitLab account, sync what you can see. That model
fits a read-only dashboard over merge requests, where every user pulls the same
public-to-them data with their own credentials.

Issue sync is not read-only. FlowLens creates, edits, closes and reopens GitLab
issues on the user's behalf. With per-user tokens the author of a GitLab issue
would be whoever happened to trigger the sync, the issue would vanish from the
app when that person's token was revoked, and a webhook — which belongs to a
GitLab project, not to a person — would have no obvious owner.

This also forces a second question: what stops the app and GitLab from
diverging, or from echoing each other's writes forever?

## Decision

**Scope the GitLab connection to the app project, not to the user.**

- One app `Project` owns exactly one `gitlab_connections` row: a base URL and an
  encrypted access token. That project may link **many** GitLab projects
  (`linked_gitlab_projects`), each with its own webhook and webhook secret.
- Authorisation stays deliberately simple for the MVP: every project carries
  `owner_user_id` and only the owner may touch it. No sharing yet. A foreign
  project returns 404, not 403.
- Secrets are encrypted at rest with AES-256-GCM (`internal/crypto`,
  `ENCRYPTION_KEY`) and never returned by the API — only the last four
  characters.

**Guarantee the task ↔ issue relationship is 1:1 in the schema, not in code.**

- `task_gitlab_links` carries `UNIQUE (linked_gitlab_project_id,
  gitlab_issue_iid)`. Two concurrent creates cannot produce two tasks for one
  issue; the database refuses.
- Webhook deliveries are deduplicated by `UNIQUE (linked_gitlab_project_id,
  delivery_uuid)` on `X-Gitlab-Event-UUID`.
- Ordering is decided by GitLab's `updated_at`, not by arrival order: an event
  no newer than `gitlab_updated_at` is skipped as stale.

## Consequences

- Issue authorship is stable and explained: issues are created by the token's
  GitLab user, which the UI shows (`token_gitlab_username`), rather than
  varying per acting user. The acting user still appears as the default
  assignee.
- A revoked or expired token breaks one project's sync visibly
  (`last_verify_error`) instead of breaking one person's view of shared data.
- Webhook ownership is unambiguous — the secret belongs to the link row, so
  `POST /webhooks/gitlab/{linkID}` can verify it in constant time without
  knowing which user is involved.
- The token is shared by everyone who can reach the project, so **project
  ownership is a real trust boundary**. Creator-only access in the MVP keeps
  that boundary trivial; adding sharing later means deciding what a
  non-owner may do with someone else's token, and that decision is deferred, not
  answered here.
- This supersedes the per-user-token expectation in `CLAUDE.md` for issue sync.
  The deferred merge-request feature may still want per-user tokens for
  read-only pulls; the two can coexist because they use different tables.
- The UI noun `Project` moves with this decision: it now means the app-level
  workspace, and the merge-request object becomes `Repository`. One noun still
  means one thing, per [ADR-0006](0006-why-ooui.md) — see
  [`docs/ui-design.md`](../ui-design.md).
- Correctness for the sync loop rests on database constraints, which are cheap
  to trust and cheap to test. The cost is that violations surface as
  constraint errors the application must expect and treat as no-ops rather than
  as failures.
