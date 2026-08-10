# FlowLens

A task tracker whose tasks stay 1:1 with GitLab CE issues, plus (in a later
phase) a view of your team's software delivery process — where merge
requests get stuck, review latency, CI results, and lead time to merge.

> **Status:** Local username/password login and the task-tracker / GitLab CE
> Issue-sync MVP are implemented: projects, backlogs, tasks, a per-project
> GitLab connection, linked GitLab projects, bidirectional sync, and an
> AI-facing task context API. See
> [GitLab CE connection & sync](#gitlab-ce-connection--sync) below. Merge
> request / CI delivery-flow visualization is not built yet (see
> [Roadmap](#roadmap)).

## Problem

Development teams track work in two places that drift apart: a GitLab CE
issue has the canonical title, description, and status, while the context an
AI coding agent actually needs — acceptance criteria, allowed/forbidden
change scope — has nowhere to live on the GitLab side. Keeping a second
tracker in sync by hand doesn't survive contact with concurrent edits,
webhook retries, or a flaky network.

## Solution

FlowLens keeps a `Task` and a GitLab CE issue in sync in both directions —
create or edit a task in FlowLens and it's pushed to GitLab; close, reopen,
or edit the issue on GitLab and it comes back — while keeping the AI-only
fields (acceptance criteria, AI context, allowed/forbidden scope) in a
separate table GitLab never sees. It does **not** generate or review code
with AI; FlowLens is the source of truth an AI agent reads from and writes
task status to, not something that writes code. The delivery-flow
visualization (review → test → merge → release) described above is a
later, separate phase — see [Roadmap](#roadmap).

## Architecture

FlowLens is a monorepo with two applications and a database.

```text
Browser ──▶ Next.js (web)  ──server-to-server──▶  Go API  ──▶  PostgreSQL
                App Router     (session cookie)     REST            │
                                                      │              │
                                          in-process sync worker ────┘
                                                      │
                                                      ▼
                                              GitLab CE REST + Webhooks
```

- **Web** (`apps/web`): Next.js App Router, React Server Components by
  default, Tailwind CSS. Server Components call the API server-to-server and
  forward the session cookie.
- **API** (`apps/api`): Go REST API. Layers are separated into HTTP
  handlers, services/use cases, a repository layer (sqlc-generated
  queries), an external GitLab CE client, and domain models. A worker running
  in the same process drains a Postgres outbox table to push task changes to
  GitLab and apply incoming webhooks — see
  [GitLab CE connection & sync](#gitlab-ce-connection--sync).
- **Database**: PostgreSQL, schema managed by `golang-migrate`.

Login (`/auth/signup`, `/auth/login`) is plain username/password with
server-side sessions — there is no OAuth redirect and no dependency on
GitLab. The GitLab connection is a separate, later step scoped to a
`Project`, not to a user (see below).

See [docs/architecture.md](docs/architecture.md) for details and
[docs/decisions](docs/decisions) for the architecture decision records.

## Technology stack

| Area            | Choice                                             |
| --------------- | -------------------------------------------------- |
| Web             | Next.js 15 (App Router), React 19, TypeScript, Tailwind CSS 4 |
| API             | Go, chi router, `net/http`                         |
| DB access       | PostgreSQL, pgx, sqlc (type-safe queries)          |
| Migrations      | golang-migrate                                     |
| Auth            | Local username/password (bcrypt), server-side sessions, HttpOnly cookie |
| GitLab sync     | Postgres outbox + in-process worker, GitLab CE REST API + webhooks |
| Secrets/crypto  | AES-256-GCM cipher behind an interface (GitLab tokens, webhook secrets) |
| Tests           | Go `testing` + testify; Vitest + React Testing Library |
| Lint            | golangci-lint; ESLint                              |
| Local infra     | Docker Compose                                     |

## Local setup

You can develop either in a **Dev Container** (recommended — no host toolchain
needed) or directly on your host with Docker Compose.

### Option A: Dev Container

Requires Docker and an editor with Dev Containers support (VS Code + the
"Dev Containers" extension, or the `devcontainer` CLI).

1. Open the repo in VS Code and run **"Dev Containers: Reopen in Container"**
   (or `devcontainer up --workspace-folder .`).
2. The container ships both toolchains (Go 1.26 + Node 22) and the `sqlc`,
   `migrate`, `air`, and `golangci-lint` CLIs. On first create it installs
   dependencies, starts Postgres, and applies migrations automatically.
3. Fill in `ENCRYPTION_KEY` in `.env` (see step 1 below), then start the app
   from inside the container:

   ```bash
   make dev-container
   ```

   This starts the API (`air`, hot reload) and web (`npm run dev`) natively
   inside the container — `docker compose` (used by `make dev`) isn't
   available there. `Ctrl+C` stops both. The forwarded ports expose the web
   app on 3000 and the API on 8080.

Inside the container Postgres is reachable at `db:5432` (the app picks this up
from the container environment). `.env` points at the host port instead
(`localhost:55432`), but the Makefile keeps the environment's `DATABASE_URL` in
preference to the `.env` value, so DB targets just work in both places:

```bash
make migrate
```

### Option B: Host + Docker Compose

### Prerequisites

- Docker + Docker Compose
- (For running tooling on the host) Go 1.26+, Node 22+, and the CLIs
  `sqlc`, `migrate`, `golangci-lint`.

### 1. Create your environment file

```bash
cp .env.example .env
```

Generate an encryption key and paste it into `.env` as `ENCRYPTION_KEY`. It
encrypts GitLab access tokens and webhook secrets at rest, and the API
refuses to start without it:

```bash
openssl rand -base64 32
```

### 2. Start the stack

```bash
make dev          # docker compose up --build
```

In another terminal, apply migrations:

```bash
make migrate
```

Then open <http://localhost:3000>, sign up with a username/password, and
create your first project. To connect it to a GitLab CE instance and sync
issues, see [GitLab CE connection & sync](#gitlab-ce-connection--sync).

## Environment variables

All variables are documented in [`.env.example`](.env.example). Key ones:

| Variable                     | Purpose                                          |
| ---------------------------- | ------------------------------------------------ |
| `DATABASE_URL`               | Postgres connection string (host uses port 55432)|
| `SESSION_TTL_HOURS`          | Session lifetime                                 |
| `ENCRYPTION_KEY`             | **Required.** base64 32-byte AES-256 key encrypting secrets at rest (GitLab access tokens, webhook secrets) — see [Generating `ENCRYPTION_KEY`](#generating-encryption_key) |
| `APP_PUBLIC_URL`             | Public URL GitLab must be able to reach to deliver webhooks. Optional; unset skips webhook auto-registration and falls back to manual sync — see [Operating without `APP_PUBLIC_URL`](#operating-without-app_public_url) |
| `SYNC_WORKER_ENABLED`        | Whether the in-process sync worker runs (default `true`) |
| `SYNC_WORKER_POLL_INTERVAL`  | Sync worker poll interval (default `5s`)         |
| `WEB_BASE_URL`               | Public URL of the web app, used for CORS         |
| `API_INTERNAL_URL`           | URL the web server uses to reach the API         |
| `NEXT_PUBLIC_API_BASE_URL`   | URL the browser uses to reach the API            |

> The container Postgres is published on host port **55432** to avoid
> clashing with a local Postgres on 5432. Inside Docker the API reaches it
> as `db:5432`.

Secrets live only in `.env`, which is git-ignored. Never commit real
credentials. In production these come from the container environment and,
later, Azure Key Vault.

## How to run

| Command             | What it does                                        |
| ------------------- | --------------------------------------------------- |
| `make setup`        | Create `.env`, install Go and web dependencies      |
| `make dev`          | Start Postgres + API + Web (hot reload, via Docker Compose) |
| `make dev-container` | Start API + Web natively inside the Dev Container (no Docker; `db` service must already be running) |
| `make migrate`      | Apply database migrations                           |
| `make generate`     | Regenerate sqlc query code                          |
| `make test`         | Run Go and web unit tests                            |
| `make test-integration` | Run Go integration tests (needs running Postgres) |
| `make lint`         | Lint Go and web                                      |
| `make build`        | Build the API binary and the web app                |
| `make build-images` | Build the production Docker images (API + web)      |
| `make down`         | Stop the stack                                       |

## Testing

- **Go**: use-case unit tests, a fake GitLab client, HTTP handler tests, and
  an integration test that runs the generated queries against a live
  Postgres (`make test-integration`, gated by the `integration` build tag).
- **Web**: API client tests, a component test, and a dashboard render test
  (Vitest).

```bash
make test
```

## Deploy

Both apps have a multi-stage production Dockerfile (`apps/api/Dockerfile`,
`apps/web/Dockerfile`) with a minimal, non-root runtime stage —
`apps/api/Dockerfile`'s `runtime` target and `apps/web/Dockerfile`'s `runner`
target, built on `output: "standalone"` in `next.config.ts` so only the
files the Next.js server actually needs (no `devDependencies`, no
unbundled `node_modules`) end up in the image.

`docker-compose.prod.yml` runs the full stack from those images:

```bash
cp .env.example .env   # fill in real values, see below
docker compose -f docker-compose.prod.yml up --build
```

Or build the images without starting them:

```bash
make build-images
```

### Build-time vs. runtime environment variables

This is the one thing to get right when deploying the web app: Next.js
inlines `NEXT_PUBLIC_*` variables into the client JavaScript bundle at
`next build` time — they cannot be changed by setting a different value on
the running container afterwards. Everything else is read at runtime.

| Variable                   | When it's needed              | Why |
| --------------------------- | ------------------------------ | --- |
| `NEXT_PUBLIC_API_BASE_URL`  | **Build time** (Docker build arg) | Baked into the browser bundle — the URL the *browser* uses to call the API. Rebuild the web image to change it. |
| `API_INTERNAL_URL`          | Runtime (container env)        | Server-only (never sent to the browser) — the URL the Next.js *server* uses to call the API, e.g. `http://api:8080` inside Compose. See `apps/web/lib/config.ts` for the client/server split. |
| `WEB_BASE_URL`, `ENCRYPTION_KEY`, `DATABASE_URL`, ... | Runtime (container env) | Read by the API process on startup, same as local dev — see [Environment variables](#environment-variables). |

`docker-compose.prod.yml` passes `NEXT_PUBLIC_API_BASE_URL` as a `build.args`
entry for exactly this reason, and `make build-images` forwards it the same
way from `.env`.

### Verifying an image locally

```bash
docker compose -f docker-compose.prod.yml up --build
curl -i http://localhost:3000/login   # expect 200
```

## GitLab CE connection & sync

Every FlowLens `Project` may connect to a GitLab CE instance and link one or
more GitLab projects to it. Once linked, tasks and GitLab issues are kept in
sync in both directions. This section is everything you need to go from a
fresh FlowLens project to a completed first import.

The connection (base URL + personal access token) is stored **per FlowLens
project, not per user** — see [ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md)
for why. Every project member who can reach the project shares the same
GitLab credentials; there is no per-user token for issue sync.

### Generating ENCRYPTION_KEY

The API refuses to start without `ENCRYPTION_KEY` — a base64-encoded 32-byte
AES-256 key used (via `internal/crypto`) to encrypt the GitLab access token
and every webhook secret before they're written to the database. Generate
one with:

```bash
openssl rand -base64 32
```

The API never returns a stored access token in a response — only its last
four characters — so losing `ENCRYPTION_KEY` means re-entering every
project's GitLab token, not losing data silently.

### Creating a GitLab personal access token

In your GitLab CE instance: **Profile → Access Tokens** (or, for a bot
account dedicated to FlowLens, that account's own access tokens).

- **Scope:** `api` — FlowLens both reads issues (initial import) and writes
  them (create/update/close/reopen), so the read-only `read_api` scope is
  not enough.
- **Role on each project you plan to link:** at least **Maintainer**.
  FlowLens registers a webhook on your behalf, and GitLab CE only allows
  Maintainer+ to manage a project's webhooks — a lower role saves the
  connection but fails to register any webhook
  (`gitlabconn.ErrInsufficientScope` / `linkedproject.ErrWebhookForbidden`,
  surfaced in the UI as a failed/unverified connection or webhook).

Paste the base URL of your GitLab CE instance (e.g.
`https://gitlab.example.com`) and the token into the project's GitLab
connection form (`PUT /api/v1/projects/{projectID}/gitlab-connection`), then
use **Test connection** (`POST .../gitlab-connection/test`) to confirm
FlowLens can reach the instance and the token is both valid and sufficiently
scoped before linking any project. **Disconnect**
(`DELETE .../gitlab-connection`) removes the stored token and, with it, every
project linked through the connection; the tasks themselves stay, as local
tasks.

### Linking a GitLab project and the initial import

From the project's GitLab connection, list the GitLab projects the token can
see (`GET .../gitlab-connection/available-projects`) and link one
(`POST .../linked-gitlab-projects`). Linking:

1. Registers a webhook on the GitLab project (see below), if
   `APP_PUBLIC_URL` is configured.
2. Enqueues an initial import (`gitlab_sync_runs.kind = 'initial_import'`)
   that pulls every in-scope issue and creates a task for each one that has
   no existing link — the reverse of a normal push, since these issues
   already existed before FlowLens knew about them. Imported issues land
   with no backlog (Unclassified / unfiled) so they can be triaged afterwards.

The first project you link becomes that FlowLens project's **default**
linked GitLab project — new tasks with no explicit link are pushed there.
Any other link can be promoted to default afterwards from its own view
(`PATCH /api/v1/linked-gitlab-projects/{linkID}` with `isDefault: true`);
there is no way to leave a connection with no default at all. A single link
reads back at `GET /api/v1/projects/{projectID}/linked-gitlab-projects/{linkID}`
— project-nested, unlike the flat `PATCH`/`DELETE`, because the link itself
carries no project in its response. Trigger a re-sync at any time from the
linked project's view (`POST .../sync-runs`, `kind = 'manual_resync'`).

### What FlowLens registers as a webhook

For each linked GitLab project, FlowLens registers exactly one webhook on
GitLab (GitLab project → **Settings → Webhooks**):

| Field | Value |
| --- | --- |
| URL | `{APP_PUBLIC_URL}/webhooks/gitlab/{linkedGitlabProjectID}` |
| Events | **Issues events** and **Comments events** only — no push, merge request, pipeline, or other events are requested |
| Secret token | A random 32-byte value FlowLens generates per link, encrypted at rest, and never re-displayed. GitLab sends it back as `X-Gitlab-Token` on every delivery, and the receiver compares it in constant time (`hmac`-style, not `==`). |

Repairing or re-registering the webhook (e.g. after it was deleted on
GitLab) is idempotent: FlowLens lists the project's existing hooks first and
updates the one at its own URL rather than creating a duplicate
(`POST /api/v1/linked-gitlab-projects/{linkID}/webhook`).

Every delivery is recorded, and the link's own view lists them newest first
for troubleshooting (`GET .../webhook-events`, paged with `?page=`/`?per_page=`
— the response's `nextPage` is `0` on the last page — and narrowed with
`?status=`). The listing omits each delivery's raw payload, which is fetched
on demand from `GET .../webhook-events/{eventID}` when a row's payload is
opened; a failed delivery can be re-applied with
`POST .../webhook-events/{eventID}/retry`.

### Task comments sync with GitLab issue discussions

A task's activity log (see "Activity log (comments)" below) rides the same
outbox/webhook machinery as its title, description and status: posting a
comment on a task linked to GitLab enqueues a `comment.create` job that posts
it to the issue as a GitLab note (`POST .../issues/:iid/notes`), and GitLab's
own `Note Hook` webhook deliveries are applied back onto the task as
`authorKind: "gitlab"` comments. A task with no GitLab link behaves exactly
as before — nothing is pushed, and no `"gitlab"`-authored comments ever
appear.

Loop prevention works differently here than for a task's own fields: because
a task can have many independently-created comments, there is no single
per-task content fingerprint to compare against (unlike
`task_gitlab_links.last_pushed_fingerprint`). Instead each `task_comments`
row stores the GitLab note id it was pushed as (or mirrored in from), and an
inbound note whose id already exists on some comment is recognized and
skipped as FlowLens's own echo rather than re-imported. GitLab-generated
system notes (e.g. "changed the description") and notes on anything other
than the linked issue are ignored.

A push failure is retried with the same backoff and eventual dead-letter
(`sync_jobs.status = 'failed'`) as every other outbound job — see "GitLab
CE connection & sync" retry/troubleshooting above.

### Editing a task's assignee and labels

A task's assignee and labels are edited from the task's single view and
mirrored to its GitLab issue through the same outbox worker as every other
field (`issue.update`). Since a GitLab assignee is a GitLab user ID, the web
app looks up candidates from the task's project's **default** linked GitLab
project:

- `GET /api/v1/linked-gitlab-projects/{linkID}/members` — the linked GitLab
  project's members, for the assignee picker (`?search=`, `?page=`/`?per_page=`).
- `GET /api/v1/linked-gitlab-projects/{linkID}/labels` — the linked GitLab
  project's existing labels, for the label picker. Labels not already on
  GitLab can still be typed in freely; they are pushed to GitLab like any
  other label on the next sync.

Both are session-only (not on the project API token's read/write allowlist —
only the web app's editing UI needs them). A project with no GitLab
connection, or no linked GitLab project yet, has no candidates to fetch: the
web app falls back to free-text entry for both fields, and the assignee field
carries no GitLab user ID until the project is connected.

### Sync scope

Each linked GitLab project has its own sync scope, set when linking and
changed later from the link's own view
(`PATCH /api/v1/linked-gitlab-projects/{linkID}`, which rewrites the scope
wholesale rather than patching single fields):

- **`all`** — every issue in the GitLab project is synced.
- **`labels`** — only issues carrying at least one of an explicit label
  list are synced; everything else is ignored on both initial import and
  webhook apply.

### Sync guarantees

- **Task ↔ issue is 1:1.** Enforced by a database `UNIQUE` constraint on
  `(linked_gitlab_project_id, gitlab_issue_iid)`, not just application code —
  two concurrent creates for the same issue cannot produce two tasks.
- **Duplicate deliveries are idempotent.** Every webhook payload carries
  GitLab's `X-Gitlab-Event-UUID`; a second delivery of the same UUID is a
  no-op 200, enforced by `UNIQUE (linked_gitlab_project_id, delivery_uuid)`.
- **Stale events are ignored.** Ordering is decided by GitLab's own
  `updated_at` on the payload, not by arrival order — an event no newer than
  the last state FlowLens applied is skipped rather than overwriting a newer
  local edit.
- **The sync loop cannot feed itself.** Applying an inbound webhook never
  enqueues an outbound push, and an inbound event whose content matches what
  FlowLens itself last pushed is recognized as an echo and skipped — so a
  task edit doesn't bounce back and forth between FlowLens and GitLab.
- **A failed push retries automatically**, with capped exponential backoff,
  from the in-process sync worker (`SYNC_WORKER_ENABLED`,
  `SYNC_WORKER_POLL_INTERVAL`). If every retry is exhausted, the task is
  marked `sync_status: "failed"` with the error visible in the UI and API,
  and can be retried manually (`POST /api/v1/tasks/{taskID}/sync-retry`).
  The same dead-letter jobs are also visible project-wide, in one place,
  from the project's own view (`GET
  /api/v1/projects/{projectID}/sync-jobs?status=failed`, session-only) and
  can be retried directly by job ID (`POST /api/v1/sync-jobs/{jobID}/retry`)
  without opening each affected task in turn.

The design behind these guarantees is recorded in
[ADR-0007](docs/decisions/0007-why-outbox-worker.md) (the outbox + worker)
and [ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md)
(the per-project connection and the 1:1 link).

### Operating without APP_PUBLIC_URL

`APP_PUBLIC_URL` is the URL GitLab must be able to reach to deliver
webhooks — it only makes sense if your FlowLens instance is reachable from
your GitLab CE instance (i.e. not `localhost` in most setups). When it's
unset:

- Webhook registration is skipped entirely; a linked GitLab project stays in
  `webhookStatus: "not_registered"` rather than failing.
- Outbound sync (FlowLens → GitLab: task create/update/close/reopen) is
  unaffected — it doesn't depend on webhooks at all.
- Inbound sync (GitLab → FlowLens) only happens when you trigger it: use
  **re-sync** on the linked project (`POST
  /api/v1/linked-gitlab-projects/{linkID}/sync-runs`) to manually pull the
  latest state from GitLab. This is a complete substitute for webhooks, just
  not automatic — nothing GitLab-side needs to change.

### API tokens

An AI agent (or any external integration) reads and writes a project's
tasks through a project-scoped bearer token rather than a user session. A
token **acts as the project's owner** — every request it makes is
authorized exactly the way that owner's session would be — but it is
confined to the one project it was issued for and to a fixed allowlist of
routes; see [ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md)
for why it's built that way.

#### Issuing a token

From the project's single view (`/projects/{projectId}`), open the **API
tokens** card, click **Issue token**, and fill in a name, a scope
(**Read-only** or **Read & write**), and an optional expiry date. The raw
token is shown exactly once, in the "Token issued" dialog right after
creation — FlowLens stores only its SHA-256 hash, the same as a session
cookie, so if you lose it you have to issue a new one. Revoke a token any
time with **Revoke** on its row in the same card.

The same action, direct against the API (session auth; this is the only
token-management route a token itself can never call — see
[What a token can't reach](#what-a-token-cant-reach) below):

```bash
curl -X POST "$API_BASE_URL/api/v1/projects/$PROJECT_ID/api-tokens" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -d '{"name": "ci-agent", "scopes": ["read", "write"]}'
```

```jsonc
{
  "id": "b7e1...",
  "projectId": "a1b2...",
  "name": "ci-agent",
  "scopes": ["read", "write"],
  "tokenPrefix": "flt_9f3a2c1d",
  "createdAt": "2026-08-02T00:00:00Z",
  "token": "flt_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d" // only ever present here
}
```

#### Calling the API

Every call authenticates with `Authorization: Bearer <token>` instead of
the session cookie:

```bash
curl "$API_BASE_URL/api/v1/tasks/$TASK_ID/context" \
  -H "Authorization: Bearer flt_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d"
```

```jsonc
// GET /api/v1/tasks/{taskID}/context
{
  "id": "3fa2...",
  "projectId": "a1b2...",
  "backlogId": "c3d4...",       // null when unfiled (Unclassified)
  "title": "Fix login redirect",
  "description": "…",
  "status": "open",             // "open" | "closed"
  "assigneeGitlabUserId": 42,   // null when unassigned
  "assigneeGitlabUsername": "octocat",
  "labels": ["bug"],
  "dueOn": "2026-08-01T00:00:00Z", // null when unset
  "updatedAt": "2026-07-30T03:00:00Z",
  "gitlab": {                   // null when never linked to a GitLab project
    "syncStatus": "synced",     // "synced" | "pending" | "failed"
    "lastError": "",
    "lastSyncedAt": "2026-07-30T03:00:00Z",
    "issueIid": 7,
    "webUrl": "https://gitlab.example.com/group/demo/-/issues/7",
    "projectPath": "group/demo"
  },
  "acceptanceCriteria": "Given/When/Then …",
  "aiContext": "Legacy payments module …",
  "allowedScope": "internal/payments/**",
  "forbiddenScope": "internal/auth/**"
}
```

A token is scoped to the project it was issued for — a request against a
task from a different project gets the same 404 a foreign session gets, not
a 403, so a token can't distinguish "not yours" from "does not exist".
`acceptanceCriteria`, `aiContext`, `allowedScope`, and `forbiddenScope` are
`""`, never `null`, until set via `PUT /api/v1/tasks/{taskID}/ai-context`.

To list several tasks at once (e.g. an agent polling its queue), use
`GET /api/v1/projects/{projectID}/tasks/context?status=open&per_page=20`,
which returns the same per-task shape plus `nextPage` (`0` when there is no
next page). `?updated_since=<RFC 3339 timestamp>` filters to tasks touched
at or after it, for incremental polling.

#### Activity log (comments)

`GET /api/v1/tasks/{taskID}/context` above also carries `comments`: the
task's most recent activity-log entries (at most 20, oldest first) — the
return path for an agent that has been reading a task's context but had no
way to report back what it did or where it got stuck. Post to that log with
`POST /api/v1/tasks/{taskID}/comments`:

```bash
curl -X POST "$API_BASE_URL/api/v1/tasks/$TASK_ID/comments" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer flt_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d" \
  -d '{"body": "Pushed a fix in MR !12; CI is green."}'
```

```jsonc
// POST /api/v1/tasks/{taskID}/comments
{
  "id": "e4a1...",
  "taskId": "3fa2...",
  "authorUserId": null,       // set for a human's comment, null for a token's
  "authorTokenId": "b7e1...", // set for a token's comment, null for a human's
  "authorKind": "agent",      // "user" | "agent" | "gitlab"
  "body": "Pushed a fix in MR !12; CI is green.",
  "createdAt": "2026-08-10T00:00:00Z",
  "updatedAt": "2026-08-10T00:00:00Z"
}
```

`GET /api/v1/tasks/{taskID}/comments` returns the full log, oldest first,
with no page cap. `DELETE /api/v1/task-comments/{commentID}` removes one
comment — a caller (session or token) can only delete its own; a
`"gitlab"`-authored comment has no user or token to match, so it cannot be
deleted through this endpoint at all.

A `"user"` or `"agent"` comment on a task linked to GitLab is also pushed to
the linked issue as a note, and a `"gitlab"`-authored comment is one GitLab
itself posted, mirrored in by the inbound webhook — see "Task comments sync
with GitLab issue discussions" above.

#### Scopes

| Scope | Grants |
| --- | --- |
| `read` | Every allowlisted `GET`, including both context endpoints above. Every token has at least this scope. |
| `write` | Everything `read` grants, plus create/update/delete on tasks, backlogs and task-dependencies. |

`scopes: ["write"]` alone is expanded to `["read","write"]` at creation —
write always implies read, so a route never has to check for `write` without
also accepting a plain `read` token where read access is all it needs.
Omitting `scopes` on creation defaults to `["read"]`.

#### Reachable endpoints

Beyond the two context endpoints above, a token can reach this fixed
allowlist of the regular task-tracker routes:

| Method | Path | Scope |
| --- | --- | --- |
| GET | `/projects` (only the token's own project, never every project its owner has) | `read` |
| GET | `/projects/{projectID}` | `read` |
| GET | `/projects/{projectID}/backlogs` | `read` |
| POST | `/projects/{projectID}/backlogs` | `write` |
| GET | `/backlogs/{backlogID}` | `read` |
| PATCH, DELETE | `/backlogs/{backlogID}` | `write` |
| GET | `/projects/{projectID}/tasks` | `read` |
| POST | `/projects/{projectID}/tasks` | `write` |
| GET | `/tasks/{taskID}` | `read` |
| PATCH, DELETE | `/tasks/{taskID}` | `write` |
| POST | `/tasks/{taskID}/close`, `/reopen`, `/assign-backlog`, `/sync-retry` | `write` |
| PUT | `/tasks/{taskID}/ai-context` | `write` |
| GET | `/tasks/{taskID}/comments` | `read` |
| POST | `/tasks/{taskID}/comments` | `write` |
| DELETE | `/task-comments/{commentID}` (own comment only) | `write` |
| GET | `/projects/{projectID}/task-dependencies` | `read` |
| POST | `/projects/{projectID}/task-dependencies` | `write` |
| DELETE | `/task-dependencies/{dependencyID}` | `write` |
| GET | `/linked-gitlab-projects/{linkID}/sync-runs` | `read` |

A single-resource URL (`{taskID}`, `{backlogID}`, `{dependencyID}`,
`{commentID}`, `{linkID}`) is checked against the token's own project the
same way `{projectID}` is: a resource in a *different* project owned by the
same user gets the same 404 as one that doesn't exist.

#### What a token can't reach

Everything else stays session-only, permanently, regardless of scope —
there is no scope that unlocks these, because each one lets an existing
credential either read a second secret or reshape the project's own trust
boundary:

- **The project's GitLab connection** (`PUT`/`GET`/`DELETE
  .../gitlab-connection`, `.../gitlab-connection/test`,
  `.../gitlab-connection/available-projects`) — it holds an encrypted GitLab
  access token; a project API token must never be a path to a second,
  more powerful secret.
- **API tokens themselves** (`GET`/`POST .../api-tokens`,
  `DELETE /api-tokens/{tokenID}`) — otherwise a read-only token could mint
  itself a write token, defeating the scope check entirely.
- **Project membership** (`GET`/`POST .../members`,
  `PATCH`/`DELETE .../members/{userID}`) — who can access a project at all
  is a project-management decision, the same reasoning as API tokens above.
- **Creating or deleting a project** (`POST /projects`,
  `DELETE /projects/{projectID}`) — a token could otherwise create or
  destroy its own footing.
- **Linked-GitLab-project management and webhook registration**
  (`POST`/`PATCH`/`DELETE .../linked-gitlab-projects*`,
  `.../webhook`, `.../webhook-events*`) — these change what the project
  syncs with and how, which is a connection-level decision, not a
  task-tracker read/write.

`Authorization` is also never added to the web app's CORS-allowed request
headers, so a bearer token stays usable only for direct, server-to-server
calls, never from browser script.

### Project membership

A project can have more than one user, each with a role — `owner`,
`member`, or `viewer` — recorded in `project_members`
([ADR-0010](docs/decisions/0010-why-project-membership.md)). Managing
membership is always owner-only, session-only (a project API token can
never reach these routes — see
[What a token can't reach](#what-a-token-cant-reach) above), and inviting
someone only adds an *existing* FlowLens user; there is no email-invite
flow (sign-up is already open).

```bash
# Add an existing user by username or email
curl -X POST "$API_BASE_URL/api/v1/projects/$PROJECT_ID/members" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -d '{"identifier": "octocat", "role": "member"}'

# List members
curl "$API_BASE_URL/api/v1/projects/$PROJECT_ID/members" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE"
```

```jsonc
// GET /api/v1/projects/{projectID}/members
[
  {
    "userId": "b7e1...",
    "username": "octocat",
    "displayName": "Octo Cat",
    "role": "member",
    "createdAt": "2026-08-02T00:00:00Z"
  }
]
```

The response never includes email — this endpoint accepts a username or
email to invite, but returning one back would let an owner use it to
enumerate registered accounts.

Two invariants apply to `PATCH`/`DELETE .../members/{userID}`: you cannot
change your own role (use another owner's session), and the project's
single designated owner (`projects.owner_user_id`) can neither be demoted
nor removed — both return `400`.

### Task & backlog scheduling, Gantt charts

Beyond GitLab issue sync, a task can carry a `startDate` (alongside the
existing `dueOn`) and predecessor/successor dependencies on other tasks in
the same project, and a **backlog** carries its own `startDate`/`dueOn` pair.
All of these are app-only: GitLab Issues have no native start-date or
dependency concept and a backlog is not a GitLab milestone, so none of them is
ever pushed to or pulled from GitLab — a task's `dueOn` remains the only date
field synced with the linked GitLab issue.

- `PATCH /api/v1/tasks/{taskID}` accepts `startDate` alongside the task's
  other mirrored fields. It is a **partial** update: a key absent from the
  body leaves that field alone, and an explicit `null` clears a nullable one
  (backlog, assignee ID, either date). That is what lets a client edit one
  attribute without echoing the whole task back — and what keeps `position`,
  which no edit form shows, from being reset. An edit that touches only
  app-only fields (`startDate`, backlog, position) enqueues no `issue.update`
  job.
- `POST /api/v1/projects/{projectID}/task-dependencies` records that
  `predecessorTaskId` must finish before `successorTaskId` starts. Both
  tasks must belong to the project, and the edge is rejected with 409 if it
  would close a cycle (checked in the application layer via a reachability
  walk, since neither a `CHECK` nor a `UNIQUE` constraint can express "no
  cycles").
- `GET /api/v1/projects/{projectID}/task-dependencies` lists every
  dependency in the project; `DELETE /api/v1/task-dependencies/{id}` removes
  one.
- `POST /api/v1/projects/{projectID}/backlogs` and
  `PATCH /api/v1/backlogs/{backlogID}` accept `startDate` and `dueOn`. On the
  PATCH the two dates are **partial** in the same sense as a task's: absent
  leaves the stored value alone, an explicit `null` clears it. That is what
  keeps a rename — which sends only name, description and position — from
  wiping the backlog's planned period. A period whose start is after its due
  date is rejected with 400 `invalid_schedule`.

In the web app, a task is created from the "New task" action on the project's
Task collection (`/projects/{projectId}/tasks`), which takes its title,
description, backlog and both dates up front. It is edited from the "Edit task"
action on the task's own single view (`/projects/{projectId}/tasks/{taskId}`),
which swaps the attribute block for an inline form covering title,
description, assignee, labels, backlog and both dates — there is no separate
edit screen, since editing is an action on the Task object (see
[`docs/ui-design.md`](docs/ui-design.md), rule 4). Predecessors and successors
are added and removed from a "Dependencies" section on the same screen; a
rejected cycle is reported there. The same collection has a
"Timeline" view mode (alongside the default "List" mode, per the OOUI rule that
a collection is one dataset presented several ways) that lays out scheduled
tasks as a Gantt chart. It is built on the shadcn `chart` component (Recharts)
so it inherits the same tokens and tooltip styling as the rest of the UI, and is
loaded on demand — the charting library stays out of the collection's bundle
until someone switches to the Timeline:

- Bars are a stacked horizontal bar chart: a transparent leading segment
  positions each task at its start date, and the visible segment spans
  start → due inclusive. A task with only one of the two dates occupies that
  single day.
- The plotted range always covers every scheduled task, so the chart never hides
  data. How much detail that range is read at is the reader's choice: a **Zoom**
  control (Month / Week / Day) sets both the width a day gets and the axis tick
  interval, and the plot scrolls horizontally rather than compressing bars into
  slivers. The initial level is derived from the span — daily ticks up to three
  weeks, then weekly, then monthly — so a short sprint and a year-long plan are
  each legible without touching the control.
- "Today" is drawn as a reference line when it falls in range, a **Today** button
  scrolls the plot back to it (disabled when today is outside the range), and a
  long project opens scrolled to today rather than to its earliest date. Changing
  the zoom magnifies around whatever was on screen instead of jumping to the
  start.
- Zoom and scroll position are local view state, exactly like the view-mode
  toggle they sit beside: they are not persisted to the URL or sent to the API,
  because the timeline redraws a collection that has already been fetched.
- A bar's colour is a status, never an identity: open work is the brand hue,
  an open task past its due date is destructive-red, and closed work recedes to
  muted. A legend names all three, so colour is never the only cue.
- Task names sit in a column beside the plot rather than as axis labels, so each
  one stays a real link to the task's single view; the bars themselves are also
  clickable.
- A project-wide closed/total progress ratio sits above the chart, and each
  task's predecessors are noted under its name. Tasks with neither a start date
  nor a due date are listed separately below the chart rather than silently
  dropped.

The Backlog collection (`/projects/{projectId}/backlogs`) has the same
Timeline mode alongside its Board and List modes, drawing one bar per
scheduled backlog with the same axis, zoom and Today controls, colours and
today marker. What it adds is **completion**: each bar is filled by
the share of that backlog's tasks that are closed, with the remainder drawn in
the same hue at low opacity, so plan and progress are read in one place. The
ratio is also stated as text (`3/8 closed (38%)`) beside the bar, in the
tooltip, and on the backlog's single view — the fill is a second reading of it,
never the only one. A backlog with no tasks reads "No tasks" and stays unfilled
rather than appearing complete, and when the task fetch itself fails the chart
says progress is unavailable instead of showing everything at 0%.

The date math lives in `apps/web/lib/timeline.ts`, separate from the components
so it is unit-testable without rendering a chart; the zoom level and scroll
position are owned by the `useTimelineViewport` hook beside it, shared by both
timelines.

### Task collection search, filters and sort

The project-scoped Task collection (`/projects/{projectId}/tasks`) narrows and
orders its List and Timeline view modes together, since both are presentations
of the same filtered set (`docs/ui-design.md` rule 5):

- A free-text box matches a task's title or description, case-insensitively.
- The status filter (All / Open / Closed) defaults to **Open**, so closed
  tasks don't fill the list; the backlog filter (unchanged) narrows further.
- Sort offers **Manual** (the API's own `position` order — the default),
  **Due date**, **Priority** and **Recently updated**, the same three
  non-manual values the cross-project Task collection's `?sort=` accepts
  (below), so the two screens agree on what each one means. Sorting is a
  display order only; it never rewrites `position`.
- All of this stays client-side — this screen already has every one of the
  project's tasks in hand (unlike the cross-project collection, which
  re-fetches from the API per filter change) — and is held in the URL
  (`?q=`, `?status=`, `?sort=`, alongside the existing `?backlog=`) the same
  way the backlog filter already was, so a reload or the browser's back
  button restores it.

### Task & backlog priority

A task and a backlog each carry a `priority` — one of `low`, `medium`, `high`,
`urgent`, defaulting to `medium` — used to decide what to work on next, ahead
of the delivery-flow dashboard planned later and the
[cross-project task collection](#cross-project-task-collection) built on it
today. Like `startDate`, task dependencies and a backlog's own dates, priority is
**app-only and never synced to GitLab**: GitLab CE issues have no native
priority field (a priority label or weight is a GitLab EE feature), so it has
no GitLab-side counterpart to push to or pull from.

- `POST`/`PATCH` on both `/api/v1/projects/{projectID}/tasks` /
  `/api/v1/tasks/{taskID}` and `/api/v1/projects/{projectID}/backlogs` /
  `/api/v1/backlogs/{backlogID}` accept `priority`. On the task PATCH,
  priority is part of the same partial-update contract as the rest of the
  task: a body without `priority` leaves the stored value alone. An absent
  `priority` on create, or an explicit empty string on either create or
  update, resets it to `medium` rather than erroring — there is no "no
  priority" state to represent. Any other value is rejected with 400
  `invalid_priority`.
- `GET .../tasks` and `GET .../backlogs` accept `?priority=low|medium|high|urgent`
  to narrow the list, and `?sort=priority` to order results by priority
  (`urgent` → `low`) instead of the manual/position order, falling back to
  that same position order to break ties between equal priorities. Both
  parameters are independent of the manual drag-reorder `position` field —
  sorting by priority is a display order for this request only and never
  rewrites `position`; see [Task & backlog reordering](#task--backlog-reordering)
  below for how the web app disables drag-to-reorder while a non-manual sort
  is active. The project-scoped task list also accepts `?sort=dueOn` (due date
  ascending, tasks with no due date last) and `?sort=updatedAt` (most recently
  updated first) — the same three values as the cross-project collection, so
  a screen's sort menu means one thing whichever list backs it. Backlogs take
  `?sort=priority` and `?sort=progress` only.

In the web app, priority is selectable wherever a task or backlog is created
or edited: the task single view's edit form, the task collection's inline
"New task" form, and — since a backlog's own rename/edit action already lives
on the Backlog collection view, not its single view, per
[`docs/ui-design.md`](docs/ui-design.md) — the backlog collection's inline
"New backlog" and per-row "Edit" forms. It is shown as a badge, the same
component for both tasks and backlogs, in list rows, timeline name columns
and the task single view. A backlog's priority is independent of its tasks':
creating or editing one never reads or writes the other. Priority is no longer
the board's axis — see [Task & backlog progress](#task--backlog-progress)
below — but it stays a badge on every board card.

### Task & backlog progress

A task and a backlog each also carry a `progress` — one of `not_started`,
`in_progress`, `on_hold`, `done`, defaulting to `not_started` — FlowLens's own
four-stage record of how far the work has got.

It is deliberately **not** a task's `status`. That field is the GitLab issue
state (`open`/`closed`) and is kept in sync both ways; `progress` is app-only
and never synced to GitLab, like `priority`, `startDate` and task
dependencies. The two never write each other: closing a task on either side
leaves its progress alone, and moving a task to `done` never closes its
GitLab issue. A task can legitimately read *Closed* and *On hold* at once, and
both badges are shown wherever either is.

- `POST`/`PATCH` on both `/api/v1/projects/{projectID}/tasks` /
  `/api/v1/tasks/{taskID}` and `/api/v1/projects/{projectID}/backlogs` /
  `/api/v1/backlogs/{backlogID}` accept `progress`, under the same
  partial-update contract as `priority`: a task PATCH without `progress`
  leaves the stored value alone, and an absent or explicitly empty value on
  create resets it to `not_started` rather than erroring. Any other value is
  rejected with 400 `invalid_progress`.
- `GET .../tasks` and `GET .../backlogs` accept
  `?progress=not_started|in_progress|on_hold|done` to narrow the list, and
  `?sort=progress` to order by progress. Progress ranks the **opposite** way
  from priority — `not_started` first through `done` — so the order reads as
  the work advancing and matches the board's left-to-right axis. Like
  `?sort=priority` it is a display order for the request only and never
  rewrites `position`. The cross-project collection `GET /api/v1/tasks`
  accepts both parameters too.

In the web app, progress is selectable everywhere priority is (both create
forms, both edit forms), and shown as its own badge beside the priority badge
in list rows, timeline name columns and the single views. A backlog's progress
is its own, set by hand — it is *not* derived from its tasks, and is separate
from the closed/total task ratio the backlog board and timeline also show.

#### The Board view mode

Both collections **present** progress as a "Board" view mode (alongside List
and Timeline, per the OOUI rule that a collection is one dataset presented
several ways): one column per stage — Not started, In progress, On hold, Done,
left to right, so the axis reads as the work advancing — with a card per object
stacked inside its column. The columns and their accents come from
`apps/web/lib/progress.ts`, so the two boards can never disagree on which way
the axis points.

- **Backlog board** (`/projects/{projectId}/backlogs`, the collection's
  *default* mode): each card shows the backlog's planned period, its priority
  badge, and its closed/total task ratio, with the ratio drawn as a fill and
  stated as text.
- **Task board** (`/projects/{projectId}/tasks`, alongside the default List
  mode): each card names the task's backlog (or Unclassified), its due date
  and assignee, its labels, and its priority, status and sync badges — the
  board's axis is progress, so neither a closed task nor an urgent one may be
  read off the column it sits in. It renders the same filtered and sorted set
  the List and Timeline modes do, so
  `?q=`/`?status=`/`?progress=`/`?backlog=`/`?sort=` narrow every mode
  together.

Dragging a card to another column changes that object's progress through the
same `PATCH /api/v1/backlogs/{backlogID}` / `PATCH /api/v1/tasks/{taskID}`,
applied optimistically and rolled back with an error if the request fails.
Dragging is the only way the board changes progress; the object's own edit form
remains the keyboard path. Everything else stays in the List mode — creating,
editing, deleting, manual reordering, and (for tasks) moving between backlogs
— since the board's one axis is progress.

### Task & backlog reordering

A task's `position` within its backlog (or the Unclassified group) and a
backlog's `position` within its project can be changed in bulk, one request
per reorder, instead of one `PATCH` per moved row:

- `PATCH /api/v1/projects/{projectID}/tasks/order` takes `{backlogId,
  taskIds}` (`backlogId: null` targets Unclassified) and resequences that
  bucket's tasks to `taskIds`' given order — position `0` for the first ID,
  `1` for the second, and so on. `taskIds` must be exactly that bucket's
  current task set (same length, no duplicates, nothing missing or foreign);
  otherwise nothing is written and the request fails with 400
  `task_ids_mismatch`, so a dropped request never leaves a bucket half
  reordered.
- `PATCH /api/v1/projects/{projectID}/backlogs/order` is the same shape for a
  project's backlogs: `{backlogIds}`, all-or-nothing, 400
  `backlog_ids_mismatch` on a mismatched set.
- Moving a task to a *different* backlog is not part of either endpoint: it
  still goes through `POST /tasks/{taskID}/assign-backlog` (or `PATCH`'s
  `backlogId`) exactly as before, followed by a `tasks/order` call for the
  destination bucket to place it at the intended position. `position` is
  app-only on both tasks and backlogs and is never synced to GitLab, the same
  as priority above — a position-only or order-only change never enqueues a
  GitLab sync job.

In the web app, the Task and Backlog collections' List views (Timeline is out
of scope) support reordering both by dragging a row and, for keyboard users,
by a pair of move-up/move-down buttons on each row — both call the same
`.../order` endpoint. A task can also be dragged onto a different backlog's
group to move it there. Reordering updates the on-screen order immediately
and only calls the API in the background (rather than this app's usual
`fetch` → `router.refresh()` pattern, which would otherwise force a full
server-component re-render per drag); a failed request reverts the order and
shows the error inline. Drag handles and move buttons are hidden while the
Task collection is sorted by anything other than the manual order, since a
drag would otherwise fight the display order it's shown in. The
drag-and-drop itself is native HTML5 drag-and-drop, not a dedicated library —
no new frontend dependency was available to add when this shipped; swapping
in a library like `@dnd-kit` later is a UI-only change, since it would still
call the same `.../order` endpoints.

### GitLab user identity

`user_gitlab_identities` maps the authenticated user to their GitLab user
ID/username on one GitLab CE instance, keyed by `(user_id, gitlab_base_url)`
since GitLab CE is self-hosted and a team may run more than one instance.
It carries no access token — that stays a distinct, still-unbuilt feature
scoped per project ([ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md))
— only the identifiers the `assignee=me` filter above needs to match against
a task's own `assigneeGitlabUserId`.

- `GET /api/v1/me/gitlab-identities` returns every identity the caller has
  registered.
- `PUT /api/v1/me/gitlab-identities` registers or replaces the identity for
  one `gitlabBaseUrl`, given `gitlabUserId` (numeric) and `gitlabUsername`.
  Both session-only, since only the logged-in web user manages their own
  identity. The web app exposes this as a form on `/settings`.
- `assigneeMe` matching is computed entirely in SQL: the task's own
  project's `gitlab_connections.base_url` is joined against the caller's
  registered identity for that same base URL. A project with no GitLab
  connection, or a caller with no registered identity, simply matches
  nothing — never an error.

### Cross-project task collection

`GET /api/v1/tasks` returns every task across every project the authenticated
user owns — "what should I be doing right now" without opening each project
in turn, and the same underlying query both `/dashboard` (below) and the
future merge-request/CI delivery-flow dashboard build on. Unlike every other
task route, it takes no `{projectID}`: it already spans every project that
owner has.

- Query parameters, all optional and independent: `status=open|closed`,
  `priority=low|medium|high|urgent`, `dueBefore=`/`dueAfter=`/`startedBefore=`
  (`YYYY-MM-DD`, inclusive), `projectId=` (repeatable — narrows within the
  caller's own projects, never a way to reach someone else's),
  `sort=dueOn|priority|updatedAt` (default `dueOn`, ascending, tasks with no
  due date last), and `assignee=me` (only tasks assigned to the caller's own
  registered GitLab identity — see [GitLab user identity](#gitlab-user-identity)
  below; a caller with no registered identity gets an empty list, not an
  error). `limit=` caps the result count (default 50, max 200); there is no
  cursor/offset pagination yet. The same `assignee=me` filter is also
  accepted on the per-project `GET .../tasks` list.
- Each task in the response carries a `projectName` field alongside every
  field `GET .../tasks/{taskID}` returns, so a cross-project list is readable
  without a second look-up per row. It never resolves GitLab sync state,
  unlike the per-project list — that would turn one query back into an N+1
  lookup; a task's own single view is still where to check that.
- **Session-only.** A project-scoped API token ([ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md))
  is issued for exactly one project and has no notion of "every project I
  own", so this route is deliberately left off the bearer-token allowlist —
  see the route registration in `internal/http/server.go`.

In the web app, `/tasks` is the cross-project Task collection (see
[`docs/ui-design.md`](docs/ui-design.md)): the default view is open tasks
with a due date, sorted soonest-first; each row links to that task's
canonical single view under its own project.

### Dashboard

`/dashboard`, the screen every login lands on, is a set of read-only teasers
built entirely from `GET /api/v1/tasks` and `GET /api/v1/projects`, not an
object of its own — it carries no edit actions, and every section links out
to the Task or Project collection it's a filtered view of
([`docs/ui-design.md`](docs/ui-design.md) rules 4/5):

- **Overdue** — open tasks whose `dueOn` is before today.
- **Due today / this week** — open tasks due between today and the end of
  this week. "This week" is Monday–Sunday and the boundary is computed from
  the web server's local time, the same convention `toApiDate` already uses
  for a picked calendar day; there is no other week-boundary convention in
  the codebase to match yet.
- **Waiting to start** — open tasks whose `startDate` has already arrived
  (`startedBefore=<today>`).
- **High priority** — open tasks with `priority` `urgent` or `high`, read off
  the same `GET /api/v1/tasks?sort=priority` ranking `?sort=priority` itself
  uses.
- **Assigned to me** — open tasks matching `GET /api/v1/tasks?assignee=me`
  (see [GitLab user identity](#gitlab-user-identity) above). Empty, not an
  error, for a user who hasn't registered their GitLab identity yet; the
  empty state points at `/settings`.
- **Sync failures** — projects with at least one task whose GitLab sync
  failed. `GET /api/v1/projects?failedSync=true` narrows to just those and
  populates `failedSyncTaskCount` for each — the plain (unfiltered) project
  list still always reports `0` there, same as `GET /api/v1/projects/{id}`
  is the only other place that count is populated. Each row links to that
  project's own view for the warning banner and retry.
- **Projects** — the most recently updated projects, linking to `/projects`.

A task with no due date never appears in the overdue/due-soon sections; if
the user has open tasks but none of them has a due date at all, those two
sections explain what setting one would surface instead of implying nothing
is due. A user with no projects yet sees a prompt to create one instead of
the sections.

## Current limitations

- The token cipher is the local AES-GCM implementation; the Azure Key Vault
  implementation is not written yet (the interface is in place).
- Integration tests assume migrations are already applied.
- The merge-request / CI delivery-flow visualization described in
  [Solution](#solution) is not built yet — see [Roadmap](#roadmap).

## Roadmap

1. **Foundation (done):** monorepo, Docker Compose, migrations, health
   check, local auth, sessions, `GET /api/v1/me`.
2. **Task tracker / GitLab CE issue sync (done):** projects, backlogs,
   tasks, per-project GitLab connection, linked GitLab projects,
   bidirectional sync, AI-facing context API — see
   [GitLab CE connection & sync](#gitlab-ce-connection--sync).
3. **Organizations & repositories:** list from GitLab, import, select active
   repositories for the merge-request feature.
4. **Merge request sync:** manual sync button, idempotent upserts, rate-limit
   and pagination handling.
5. **Dashboard & MR views:** metrics, list with filters, detail page,
   empty/loading/error states.
6. **Automation:** webhooks (with duplicate-delivery handling) and scheduled
   sync via Azure Service Bus.
7. **Azure deployment:** Container Apps, Azure Database for PostgreSQL, Key
   Vault, Application Insights.
