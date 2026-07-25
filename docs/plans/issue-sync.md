# MVP spec — task management with GitLab CE Issue sync

> **Status: agreed, not yet implemented.** This document is the design the MVP
> is built against. It supersedes nothing; the merge-request / CI delivery-flow
> feature stays in the roadmap but is out of scope for this MVP.
>
> This is a **plan** — see [`README.md`](README.md) for how plans are handled.
> It has a finite life and is deleted once the seven phases below ship. The
> decisions meant to outlive it are already recorded as
> [ADR-0007](../decisions/0007-why-outbox-worker.md) (outbox + worker) and
> [ADR-0008](../decisions/0008-why-per-project-gitlab-connection.md)
> (per-project GitLab connection, 1:1 task ↔ issue link).

FlowLens gains a task tracker whose tasks are kept **1:1 with GitLab CE issues**
in both directions, plus app-only fields that describe a task for an AI agent
(acceptance criteria, context, allowed and forbidden change scope).

## Decisions taken up front

| Question | Decision |
| --- | --- |
| Relationship to MR/CI visualisation | Both features coexist; issue sync ships first. Existing `repositories` / `pull_requests` / `sync_runs` tables are left untouched. |
| App project ↔ GitLab project | One app `Project` links to **many** GitLab projects. |
| GitLab URL + access token | Stored **per app project** (`gitlab_connections`, one row per project). Webhook secret is per linked GitLab project. |
| Authorisation | Creator-only. Every project carries `owner_user_id`; no sharing in the MVP. |
| Sync execution | **Outbox table + in-process Go worker** (Postgres only, no Redis). |
| Task deletion | Deleting a task never touches GitLab. The link is remembered so re-sync does not resurrect it. |
| Backlogs | App-only. Never mapped to milestones or labels. |
| Assignee | Defaults to the acting user's linked GitLab account, changeable to any member of the GitLab project. |
| AI context API | Cookie session **and** project-scoped bearer API tokens. |
| Webhook delivery | `APP_PUBLIC_URL` is assumed to be reachable by GitLab; when unset, webhook registration is skipped and manual sync covers the gap. |
| Deliverable | Go API + Next.js UI. |

## Object model

Added to the table in [`ui-design.md`](./ui-design.md):

| Object | Meaning | Backing table |
| --- | --- | --- |
| `Project` | A workspace owned by one user: backlogs, tasks, one GitLab connection | `projects` |
| `Backlog` | An app-only grouping of tasks inside a project | `backlogs` |
| `Task` | One unit of work, optionally mirrored by a GitLab issue | `tasks` (+ `task_ai_contexts`, `task_gitlab_links`) |
| `GitLabConnection` | A GitLab CE base URL and access token for one project | `gitlab_connections` |
| `LinkedGitLabProject` | A GitLab project this app project syncs issues with | `linked_gitlab_projects` |
| `SyncRun` | One import / re-sync attempt against a linked GitLab project | `gitlab_sync_runs` |
| `WebhookEvent` | One received GitLab webhook delivery and its processing state | `webhook_events` |

**Naming conflict, resolved deliberately:** `ui-design.md` currently maps the UI
noun `Project` to the GitHub-era `repositories` table (the MR feature). That
mapping moves: `Project` is now the app-level workspace, and the deferred MR
object is renamed `Repository` in the UI vocabulary. The GitLab-side project is
`LinkedGitLabProject`, never plain "Project", so one noun still means one thing.

Until phase 7 lands, [`ui-design.md`](../ui-design.md) still carries the old
definition and is the authority for the merge-request feature; it links here for
the new one. Rationale in
[ADR-0008](../decisions/0008-why-per-project-gitlab-connection.md).

### Why the task is split across three tables

The requirement is that GitLab-owned data and app-owned data stay separated.

- `tasks` — the **mirrored** fields, the ones both sides may write: title,
  description, status, assignee, labels, due date.
- `task_ai_contexts` — **app-only**, never sent to GitLab: acceptance criteria,
  AI context, allowed change scope, forbidden change scope.
- `task_gitlab_links` — the **link and sync state**: which issue, what GitLab
  last told us, sync status, last error.

A task with no link row is a purely local task; it gets an issue as soon as its
project has a linked GitLab project (see below).

## Schema (migration `000003_issue_sync`)

```
projects(id, owner_user_id → users, name, description, created_at, updated_at)
  UNIQUE (owner_user_id, name)

backlogs(id, project_id → projects, name, description, position, created_at, updated_at)

tasks(id, project_id → projects,
      backlog_id → backlogs ON DELETE SET NULL,   -- NULL = 未分類
      title, description, status('open'|'closed'), closed_at,
      assignee_gitlab_user_id, assignee_gitlab_username,
      labels TEXT[], due_on DATE, position,
      created_by_user_id → users, created_at, updated_at)
  INDEX (project_id, backlog_id), (project_id, status)

task_ai_contexts(task_id PK → tasks, acceptance_criteria, ai_context,
                 allowed_scope, forbidden_scope, updated_at)

task_gitlab_links(task_id PK → tasks,
                  linked_gitlab_project_id → linked_gitlab_projects,
                  gitlab_issue_id, gitlab_issue_iid, gitlab_web_url,
                  gitlab_updated_at,          -- newest GitLab state we applied
                  last_pushed_fingerprint,    -- hash of what we last sent
                  sync_status('synced'|'pending'|'failed'),
                  last_error, last_synced_at)
  UNIQUE (linked_gitlab_project_id, gitlab_issue_iid)   -- the 1:1 guarantee

gitlab_connections(id, project_id UNIQUE → projects, base_url,
                   encrypted_token, token_gitlab_user_id, token_gitlab_username,
                   last_verified_at, last_verify_error, created_at, updated_at)

linked_gitlab_projects(id, gitlab_connection_id → gitlab_connections,
                       gitlab_project_id, path_with_namespace, name, web_url,
                       sync_scope('all'|'labels'), sync_labels TEXT[],
                       webhook_id, encrypted_webhook_secret, webhook_registered_at,
                       initial_import_status, last_synced_at, created_at, updated_at)
  UNIQUE (gitlab_connection_id, gitlab_project_id)

gitlab_sync_runs(id, linked_gitlab_project_id → linked_gitlab_projects,
                 kind('initial_import'|'manual_resync'), status,
                 issues_seen, issues_created, issues_updated,
                 started_at, completed_at, error_message, created_at)

webhook_events(id, linked_gitlab_project_id → linked_gitlab_projects,
               delivery_uuid, event_name, object_kind, gitlab_issue_iid,
               payload JSONB, gitlab_updated_at,
               status('pending'|'processed'|'skipped'|'failed'),
               skip_reason, error_message, received_at, processed_at)
  UNIQUE (linked_gitlab_project_id, delivery_uuid)   -- duplicate deliveries

sync_jobs(id, project_id → projects, task_id → tasks NULL,
          kind, payload JSONB, dedupe_key UNIQUE NULL,
          status('pending'|'running'|'succeeded'|'failed'),
          attempts, run_after, last_error, created_at, updated_at)
  INDEX (status, run_after)

project_api_tokens(id, project_id → projects, name, token_hash UNIQUE,
                   last_used_at, expires_at, created_at)
```

All new tables use UUID PKs, `timestamptz`, and cascade on delete, matching the
existing schema conventions. Run `make generate` after the migration lands.

## Sync engine

### Outbound (app → GitLab)

Handlers never call GitLab inline. A mutation writes the task and enqueues a
`sync_jobs` row **in the same transaction**, so an accepted request is always
eventually pushed.

| Job kind | Trigger |
| --- | --- |
| `issue.create` | Task created in a project that has ≥1 linked GitLab project |
| `issue.update` | Title / description / assignee / labels / due date changed |
| `issue.close`, `issue.reopen` | Task status changed |
| `project.import` | A GitLab project is linked (initial import) |
| `project.resync` | Manual re-sync requested |
| `webhook.register` | A GitLab project is linked, or its webhook is repaired |

The worker claims jobs with `SELECT … FOR UPDATE SKIP LOCKED`, retries with
exponential backoff (capped attempts), and on final failure sets
`task_gitlab_links.sync_status = 'failed'` with the error text. `dedupe_key`
(e.g. `issue.update:<task_id>`) collapses rapid repeated edits into one pending
job.

Which linked project receives a new task's issue: the project's **default**
linked GitLab project (the first one linked, changeable per project). A task
already linked always pushes to its own linked project.

### Inbound (GitLab → app)

`POST /webhooks/gitlab/{linked_project_id}` is unauthenticated but verified by
comparing `X-Gitlab-Token` against the decrypted per-link webhook secret in
constant time. The handler **only records** the event (`webhook_events`,
`status='pending'`) and returns 200 fast; the worker applies it.

Three guards, each with its own test:

1. **Duplicate delivery** — `UNIQUE (linked_gitlab_project_id, delivery_uuid)`
   on `X-Gitlab-Event-UUID`. A conflicting insert is a no-op 200. The
   `UNIQUE (linked_gitlab_project_id, gitlab_issue_iid)` on the link table is
   the second line of defence: two concurrent creates cannot produce two tasks.
2. **Stale event** — an event is skipped (`skip_reason='stale'`) when its
   payload `updated_at` is not newer than `task_gitlab_links.gitlab_updated_at`.
   Ordering is decided by GitLab's timestamps, not by arrival order.
3. **Loop prevention** — applying an inbound event **never enqueues an outbound
   job**; the apply path writes through a repository method that takes no
   outbound side effects. Additionally, an event whose content fingerprint
   equals `last_pushed_fingerprint` is skipped (`skip_reason='echo'`), so our
   own write coming back does not even bump timestamps.

Issue `close` / `reopen` map to `tasks.status`. Imported issues that match no
existing task become tasks with `backlog_id = NULL` (未分類), assignable to a
backlog afterwards.

### Sync scope

Per linked GitLab project: `all`, or `labels` with an explicit label list.
Issues outside the scope are ignored on import and on webhook apply.

## API surface

```
# Project / backlog / task (session auth, owner only)
GET|POST      /api/v1/projects
GET|PATCH|DELETE /api/v1/projects/{projectID}
GET|POST      /api/v1/projects/{projectID}/backlogs
GET|PATCH|DELETE /api/v1/backlogs/{backlogID}
GET|POST      /api/v1/projects/{projectID}/tasks        ?backlog_id=|unassigned|status=
GET|PATCH|DELETE /api/v1/tasks/{taskID}
POST          /api/v1/tasks/{taskID}/close | /reopen
POST          /api/v1/tasks/{taskID}/sync-retry
PUT           /api/v1/tasks/{taskID}/ai-context

# GitLab connection
PUT|GET|DELETE /api/v1/projects/{projectID}/gitlab-connection
POST           /api/v1/projects/{projectID}/gitlab-connection/test     -- reachability + token validity
GET            /api/v1/projects/{projectID}/gitlab-connection/available-projects
GET|POST       /api/v1/projects/{projectID}/linked-gitlab-projects
PATCH|DELETE   /api/v1/linked-gitlab-projects/{linkID}
GET|POST       /api/v1/linked-gitlab-projects/{linkID}/sync-runs       -- manual re-sync
GET            /api/v1/linked-gitlab-projects/{linkID}/webhook-events

# AI-facing (session OR `Authorization: Bearer <project token>`)
GET  /api/v1/tasks/{taskID}/context      -- GitLab issue fields + AI fields in one payload
GET  /api/v1/projects/{projectID}/tasks/context ?status=&backlog_id=
GET|POST|DELETE /api/v1/projects/{projectID}/api-tokens

# Webhook (token-header auth)
POST /webhooks/gitlab/{linkID}
```

Errors follow the existing `internal/http/response.go` helpers. Every task
response carries `sync_status`, `last_error` and `gitlab_web_url` so the UI can
show per-task sync state and offer retry.

## Web screens (OOUI)

Collection + single view per object, per [`ui-design.md`](./ui-design.md):

- `/projects` → `/projects/{id}` — identity, then attributes, then related
  collections: backlogs, tasks, linked GitLab projects, recent sync runs.
  Actions on the project: connect GitLab, test connection, re-sync now.
- `/backlogs/{id}` — one backlog and its tasks; unfiled tasks appear as the
  "未分類" grouping in the project's task collection, not as their own screen.
- `/tasks/{id}` — identity (title, status, sync badge) → attributes (assignee,
  labels, due date, GitLab link) → AI context section → retry action on failure.
- GitLab connection and API tokens live inside the project's single view, not on
  a separate "setup" screen (rule 4; the auth exemption does not extend here).

## Security

- The GitLab access token and every webhook secret are encrypted at rest with
  **AES-256-GCM** in a new `internal/crypto` package; the key comes from
  `ENCRYPTION_KEY` (base64, 32 bytes) and is required at startup. Tokens are
  never returned by the API — responses expose only the last four characters.
- Project API tokens are opaque and stored as SHA-256 hashes, like sessions.
- Webhook token comparison uses `hmac.Equal`; the endpoint is rate-limited and
  bounded in body size.
- Every project-scoped handler checks `owner_user_id` against the session user;
  a foreign project returns 404, not 403.

New config: `ENCRYPTION_KEY` (required), `APP_PUBLIC_URL` (optional; webhook
registration is skipped without it), `SYNC_WORKER_ENABLED`, `SYNC_WORKER_POLL_INTERVAL`.

## Testing

Following [`testing.md`](./testing.md) — Fakes over mocks, table-driven cases:

- `gitlab.FakeClient` grows the issue/webhook/member methods and records calls,
  so outbound behaviour is asserted without HTTP.
- Domain tests: scope filtering, fingerprinting, status mapping, assignee
  defaulting.
- HTTP tests: webhook signature rejection, duplicate delivery, stale event,
  echo suppression, authorisation on every project-scoped route.
- Integration tests (`integration` tag): migration + outbox worker round trip,
  initial import idempotency, unique-constraint enforcement of the 1:1 link.

README gains a "GitLab CE connection & sync" section: required env, how to
create the PAT (`api` scope), what the app registers as a webhook, the sync
scope options, and the loop/dup/stale guarantees.

## Phases

1. Migration + sqlc + `internal/crypto` + config.
2. Project / backlog / task CRUD + AI context (API and UI).
3. GitLab connection: save, verify, list available projects, link, register webhook.
4. Outbox worker + outbound create/update/close/reopen + sync status and retry.
5. Webhook receiver, apply pipeline with the three guards, initial import, manual re-sync.
6. AI context API + project API tokens.
7. README, `ui-design.md` object-table update (drop the pending-redefinition
   note and rename the MR object to `Repository`), then **delete this plan** —
   ADR-0007 and ADR-0008 already carry the decisions.
