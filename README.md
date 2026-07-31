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
from the container environment). Makefile DB targets read `.env`, which points
at the host port, so pass the container URL explicitly when needed:

```bash
make migrate DATABASE_URL="$DATABASE_URL"
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
scoped before linking any project.

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
   with no backlog (未分類 / unfiled) so they can be triaged afterwards.

The first project you link becomes that FlowLens project's **default**
linked GitLab project — new tasks with no explicit link are pushed there.
Trigger a re-sync at any time from the linked project's view
(`POST .../sync-runs`, `kind = 'manual_resync'`).

### What FlowLens registers as a webhook

For each linked GitLab project, FlowLens registers exactly one webhook on
GitLab (GitLab project → **Settings → Webhooks**):

| Field | Value |
| --- | --- |
| URL | `{APP_PUBLIC_URL}/webhooks/gitlab/{linkedGitlabProjectID}` |
| Events | **Issues events** only — no push, merge request, pipeline, or other events are requested |
| Secret token | A random 32-byte value FlowLens generates per link, encrypted at rest, and never re-displayed. GitLab sends it back as `X-Gitlab-Token` on every delivery, and the receiver compares it in constant time (`hmac`-style, not `==`). |

Repairing or re-registering the webhook (e.g. after it was deleted on
GitLab) is idempotent: FlowLens lists the project's existing hooks first and
updates the one at its own URL rather than creating a duplicate
(`POST /api/v1/linked-gitlab-projects/{linkID}/webhook`).

### Sync scope

Each linked GitLab project has its own sync scope, set when linking or
changed later (`PATCH /api/v1/linked-gitlab-projects/{linkID}`):

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

### AI-facing API

An AI agent (or any external integration) reads and writes a task's status
through a project-scoped bearer token rather than a user session:

1. Issue a token from the project's single view or
   `POST /api/v1/projects/{projectID}/api-tokens` (session auth). The raw
   token is shown exactly once, at creation — FlowLens stores only its
   SHA-256 hash, the same as a session cookie.
2. Call the context API with `Authorization: Bearer <token>`:

```jsonc
// GET /api/v1/tasks/{taskID}/context
{
  "id": "3fa2...",
  "projectId": "a1b2...",
  "backlogId": "c3d4...",       // null when unfiled (未分類)
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

### Task scheduling & Gantt chart

Beyond GitLab issue sync, a task can carry a `startDate` (alongside the
existing `dueOn`) and predecessor/successor dependencies on other tasks in
the same project. Both are app-only: GitLab Issues have no native start-date
or dependency concept, so neither is ever pushed to or pulled from GitLab —
`dueOn` remains the only date field synced with the linked GitLab issue.

- `PATCH /api/v1/tasks/{taskID}` accepts `startDate` alongside the task's
  other mirrored fields.
- `POST /api/v1/projects/{projectID}/task-dependencies` records that
  `predecessorTaskId` must finish before `successorTaskId` starts. Both
  tasks must belong to the project, and the edge is rejected with 409 if it
  would close a cycle (checked in the application layer via a reachability
  walk, since neither a `CHECK` nor a `UNIQUE` constraint can express "no
  cycles").
- `GET /api/v1/projects/{projectID}/task-dependencies` lists every
  dependency in the project; `DELETE /api/v1/task-dependencies/{id}` removes
  one.

In the web app, a task is created from the "New task" action on the project's
Task collection, which takes its title, description, backlog and both dates up
front. The same collection has a "Timeline" view mode
(alongside the default "List" mode, per the OOUI rule that a collection is
one dataset presented several ways) that lays out scheduled tasks as a Gantt
chart. It is built on the shadcn `chart` component (Recharts) so it inherits
the same tokens and tooltip styling as the rest of the UI, and is loaded on
demand — the charting library stays out of the project page's bundle until
someone switches to the Timeline:

- Bars are a stacked horizontal bar chart: a transparent leading segment
  positions each task at its start date, and the visible segment spans
  start → due inclusive. A task with only one of the two dates occupies that
  single day.
- The date axis switches from daily to weekly to monthly ticks as the project's
  span grows, and the plot scrolls horizontally rather than compressing bars
  into slivers. "Today" is drawn as a reference line when it falls in range.
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

The date math lives in `apps/web/lib/timeline.ts`, separate from the components
so it is unit-testable without rendering a chart.

## Current limitations

- The token cipher is the local AES-GCM implementation; the Azure Key Vault
  implementation is not written yet (the interface is in place).
- CSRF protection for the API relies on `SameSite=Lax` cookies plus a
  locked-down CORS origin; a double-submit token is planned.
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
