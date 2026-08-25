# FlowLens

A self-hosted task tracker — backlogs, tasks, board, Gantt timeline, and
delivery metrics — that can optionally keep every task 1:1 with a GitLab CE
issue, and read your merge request and CI data to show where delivery
actually gets stuck.

It runs standalone: **GitLab is optional.** Connect it and tasks and issues
stay in sync both ways; leave it unconnected and FlowLens is a task tracker
with nothing else to configure.

```bash
curl -O https://raw.githubusercontent.com/Motoki1996/flowlens/main/compose.yaml
curl -o .env https://raw.githubusercontent.com/Motoki1996/flowlens/main/.env.example
docker run --rm ghcr.io/motoki1996/flowlens-api:latest gen-key >> .env
docker compose up -d          # http://localhost:4000
```

Set `POSTGRES_PASSWORD` and pin `FLOWLENS_VERSION` in `.env` first. Full
install, upgrade, backup and hardening instructions are in
[**docs/self-hosting.md**](docs/self-hosting.md).

## What you get

- **Tasks and backlogs** with priority, a four-stage progress state,
  start/due dates, dependencies, drag-and-drop ordering, and an activity log.
- **Four ways to look at them** — List, Board (by progress), Timeline
  (Gantt), and a cross-project view — plus a dashboard of what is overdue,
  due soon, waiting to start, or failing to sync.
- **An API built for AI agents.** Project-scoped bearer tokens and a task
  context endpoint that carries acceptance criteria and the backlog's
  allowed/forbidden change scope — fields GitLab has nowhere to put.
  FlowLens does not write code; it is the source of truth an agent reads
  from and reports back to.
- **Optional GitLab CE sync.** Bidirectional issue sync through a Postgres
  outbox and webhooks, read-only merge request and pipeline import, and
  delivery metrics (review latency median/p90, pipeline success rate, merge
  throughput) computed over what it synced.

## Problem

Development teams track work in two places that drift apart: a GitLab CE
issue has the canonical title, description, and status, while the context an
AI coding agent actually needs — acceptance criteria on the task, an
allowed/forbidden change scope on the backlog — has nowhere to live on the
GitLab side. Keeping a second tracker in sync by hand doesn't survive
contact with concurrent edits, webhook retries, or a flaky network.

## Solution

FlowLens keeps a `Task` and a GitLab CE issue in sync in both directions —
create or edit a task in FlowLens and it's pushed to GitLab; close, reopen,
or edit the issue on GitLab and it comes back — while keeping the AI-only
fields (a task's acceptance criteria and AI context, a backlog's
allowed/forbidden scope) in tables GitLab never sees. It does **not**
generate or review code with AI; FlowLens is the source of truth an AI
agent reads from and writes
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
   app on 4000 and the API on 8080.

   It also warms the web dev server in the background: Next.js compiles a
   route the first time it is requested, so without this the first screen
   you open after every restart waits for it. `make dev-container` requests
   the common routes itself while you are still switching to the browser.
   Set `FLOWLENS_DEV_WARM=0` to turn that off.

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

Then open <http://localhost:4000>, sign up with a username/password, and
create your first project. To connect it to a GitLab CE instance and sync
issues, see [GitLab CE connection & sync](#gitlab-ce-connection--sync).

## Environment variables

All variables are documented in [`.env.example`](.env.example), and the ones
that matter for a self-hosted install are tabulated in
[docs/self-hosting.md](docs/self-hosting.md#configuration-reference). Key
ones for development:

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
| `ALLOW_SIGNUP`               | Whether new accounts can be registered (default `true`) |
| `TRUSTED_PROXY_HOPS`         | Proxies trusted to have appended to `X-Forwarded-For`, which the per-IP rate limiters key on |
| `RUN_MIGRATIONS`             | Apply the embedded migrations at startup (default `true`) |
| `NEXT_PUBLIC_API_BASE_URL`   | URL the browser uses to reach the API. Empty by default — the browser calls the web app's own origin and Next.js proxies through to the API |

> The container Postgres is published on host port **55432** to avoid
> clashing with a local Postgres on 5432. Inside Docker the API reaches it
> as `db:5432`.

Secrets live only in `.env`, which is git-ignored. Never commit real
credentials; in production they come from the container environment.

## How to run

| Command             | What it does                                        |
| ------------------- | --------------------------------------------------- |
| `make setup`        | Create `.env`, install Go and web dependencies      |
| `make dev`          | Start Postgres + API + Web (hot reload, via Docker Compose) |
| `make dev-container` | Start API + Web natively inside the Dev Container (no Docker; `db` service must already be running) |
| `make migrate`      | Apply database migrations by hand (the API also applies them on startup) |
| `make generate`     | Regenerate sqlc query code                          |
| `make test`         | Run Go and web unit tests                            |
| `make test-integration` | Run Go integration tests (needs running Postgres) |
| `make lint`         | Lint Go and web                                      |
| `make build`        | Build the API binary and the web app                |
| `make build-images` | Build the release images locally, tagged `:dev`     |
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

Self-hosting — install, upgrade, backup, hardening, air-gapped networks —
has its own guide: [**docs/self-hosting.md**](docs/self-hosting.md). The
short version is at the top of this file.

What the deployment is made of:

- **Prebuilt multi-arch images** (amd64 + arm64) on
  `ghcr.io/motoki1996/flowlens-{api,web}`, published by
  [`.github/workflows/release.yml`](.github/workflows/release.yml) on a
  `v*` tag. Both are minimal, non-root runtime stages —
  `apps/api/Dockerfile`'s `runtime` target, and `apps/web/Dockerfile`'s
  `runner` target built on `output: "standalone"` so only the files the
  Next.js server actually needs are in the image.
- **[`compose.yaml`](compose.yaml)**, the file a self-hoster downloads. It
  pulls those images and needs nothing else from this repository.
  [`compose.tls.yaml`](compose.tls.yaml) overlays HTTPS via Caddy.
- **Self-applying schema.** The API embeds its migrations
  (`apps/api/migrations/embed.go`) and applies them on startup, so there is
  no separate migrate step and no `migrate` CLI on the host. Set
  `RUN_MIGRATIONS=false` where migrations are their own deploy stage.

To run images you built yourself:

```bash
make build-images                                     # tags them :dev
FLOWLENS_VERSION=dev docker compose -f compose.yaml up -d
```

### One origin, and why

The browser only ever talks to the web app's origin. The Next.js server
proxies `/api`, `/auth` and `/webhooks` through to the Go API
(`apps/web/next.config.ts`), and `compose.yaml` does not publish the API
port at all.

This is not just tidiness. `NEXT_PUBLIC_*` variables are inlined into the
client bundle at `next build` time and cannot be changed on a running
container — so a web image built with an absolute API URL would only ever
work for the hostname it was built for, and every self-hoster would have to
rebuild it. Same-origin means one published image serves everyone. It also
means there is no CORS to configure, no `SameSite=Lax` cross-site problem to
work around, and the operational endpoints (`/healthz`, `/version`,
`/metrics`) are not exposed on the public origin.

Next.js resolves rewrites at build time too, so the destination
(`API_INTERNAL_URL`, default `http://api:8080`) is likewise baked into the
image. That is fine here for the reason the public URL is not: it is an
address on the internal Docker network, and it is the same for every
self-hoster running the bundled `compose.yaml`. Reaching the API at some
other address means rebuilding the web image with `API_INTERNAL_URL` set —
which is what `make dev` and the e2e suite do. Server Components are
unaffected either way: `lib/api.ts` reads `API_INTERNAL_URL` at request
time.

Setting `NEXT_PUBLIC_API_BASE_URL` is still supported for putting the API on
its own hostname. Then it is a build arg, `WEB_BASE_URL` must match it for
CORS, and both origins have to stay on one registrable domain for the
session cookie to be sent.

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

### TLS for a self-hosted instance

An on-prem GitLab CE typically presents a certificate signed by a private CA
(or a self-signed one) that the API container has no reason to trust. Go
verifies against the system roots by default, so the handshake fails before
any request is sent and the connection form can only report it as
`unreachable`. Two environment variables control this, and they apply to
**GitLab only** — the client's TLS policy is passed in at wiring time
(`gitlab.TLSPolicy`), so a future GitHub client keeps verifying normally.

| Variable | Default | Effect |
| --- | --- | --- |
| `GITLAB_TLS_INSECURE_SKIP_VERIFY` | `true` | Skips certificate verification entirely. Only safe on a network you already trust — it leaves the connection open to interception. The API logs a warning at startup while it is on. |
| `GITLAB_CA_CERT_FILE` | *(unset)* | Path to a PEM bundle added to the system roots. **Takes precedence**: naming a CA turns verification back on, so a configured CA can never silently degrade into no verification. |

Preferring the CA file is the better setup wherever the CA is available:

```bash
# docker-compose.yml already reads both; mount the CA into the api service.
GITLAB_CA_CERT_FILE=/etc/ssl/certs/corp-ca.pem
```

A certificate the policy still rejects is reported distinctly from a network
failure — 422 `tls_error` rather than `unreachable`, with the underlying
x509 error in the API log — and is not retried, since retrying only repeats
the same handshake.

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

### A backlog's own GitLab project

A project's default link is project-wide, but a team often keeps one backlog's
work in a different GitLab repository. A backlog can therefore name its own
destination for new issues — `defaultLinkedGitlabProjectId` on
`POST /api/v1/projects/{projectID}/backlogs` and
`PATCH /api/v1/backlogs/{backlogID}`, editable from the create/edit form on the
Backlog collection screen and shown on the Backlog single view.

Creating a task resolves where its GitLab issue goes in this order:

1. the task's backlog's own linked GitLab project, if it names one;
2. otherwise the project's **default** linked GitLab project;
3. otherwise nowhere — the task stays purely local.

Two rules make this predictable:

- **It is read only at task-create time.** Once an issue exists, its linked
  project is recorded in `task_gitlab_links` and every later update, close and
  reopen follows that row, so moving a task between backlogs afterwards never
  moves the issue or re-targets it. FlowLens does not move GitLab issues
  between projects.
- **The link must belong to the same FlowLens project.** A link from another
  project is rejected with 400 `invalid_linked_gitlab_project`. Unlinking a
  GitLab project doesn't delete the backlogs that named it — they fall back to
  the project default (`ON DELETE SET NULL`).

Inbound sync is unchanged: an issue imported or delivered by webhook still
lands with no backlog (Unclassified), whichever GitLab project it came from.

### A backlog's base branch

A backlog can also name the branch its tasks are meant to branch from during
development (e.g. `main`, `release/2.4`) — `baseBranch` on
`POST /api/v1/projects/{projectID}/backlogs` and
`PATCH /api/v1/backlogs/{backlogID}`, editable from the same create/edit form
and shown on the Backlog single view. It is optional, validated as a git
branch name when non-empty (400 `invalid_base_branch` otherwise), app-only,
and never synced to or from GitLab — unlike a merge request's own base
branch, which mirrors an actual GitLab merge request. It is also surfaced on
`GET /api/v1/tasks/{taskID}/context` (resolved through the task's backlog, or
`""` if unfiled or unset), so an AI agent working a task knows what branch to
start from.

### A backlog's allowed/forbidden change scope

A backlog can also name the paths its tasks may and may not touch —
`allowedScope`/`forbiddenScope` on the same create/edit endpoints and form,
and shown on the Backlog single view. These used to be per-task fields on
`task_ai_contexts`, but in practice they describe a sub-area of the
codebase rather than one unit of work, so they moved to the backlog: set
once, they apply to every task filed in it. Optional, capped at 20000
characters (400 `invalid_scope` otherwise), app-only, and never synced to
GitLab. Like `baseBranch`, they are surfaced on
`GET /api/v1/tasks/{taskID}/context` resolved through the task's backlog
(`""` if unfiled or unset) — a task's own `acceptanceCriteria`/`aiContext`
(set via `PUT /api/v1/tasks/{taskID}/ai-context`) remain the place for
anything task-specific.

### Epics: an optional layer between a backlog and its tasks

A refined backlog is not always broken straight down into
implementation-sized tasks. The work is usually first cut into coarse units —
one screen, one endpoint group, one migration + API pair — and only then is
each of those broken into tasks someone actually works. That coarse unit is
an **epic**.

It is optional in both directions: a task may sit directly in a backlog
exactly as before, and an epic may sit outside any backlog. Nothing about an
existing project changes until someone creates one.

- `GET`/`POST /api/v1/projects/{projectID}/epics`
  (`?backlog_id=`/`?priority=`/`?progress=`/`?assignee=`/`?sort=`),
  `PATCH /api/v1/projects/{projectID}/epics/order`, and
  `GET`/`PATCH`/`DELETE /api/v1/epics/{epicID}` — the same route shape,
  bearer-token allowlisting and scopes a backlog's endpoints have.
- The web app's screens are `/projects/[projectId]/epics` (Board / List /
  Timeline view modes, Board by default) and
  `/projects/[projectId]/epics/[epicId]`.
- A task names its epic with `epicId` on create, update and bulk create, and
  the task collections take `?epic_id=<uuid>|unassigned`.

An epic carries the same fields a backlog does — name, description,
position, start/due dates, priority, progress, assignee, base branch,
allowed/forbidden scope and its own `defaultLinkedGitlabProjectId` — minus
`size`, since an epic's size is just the sum of its tasks'. It is app-only:
**no epic is ever created in, or read from, GitLab.** GitLab CE has no Epic
at all (it is a Premium object), which is also why the name was free to take.

A task is linked to an epic from either side, and both write the same
`tasks.epic_id`:

- **From the task** — `epicId` on task create/update, and the Epic control on
  the Task single view.
- **From the epic** — `PATCH /api/v1/epics/{epicID}/tasks` with the epic's
  complete `taskIds` set: what it names is filed under the epic, what it no
  longer names drops out (keeping its backlog). All-or-nothing, so a task from
  another project moves nothing. The web app offers it as a checkbox list of
  the backlog's free tasks, on the epic's create/edit form and on its single
  view.

Two rules the schema can't express are enforced by the API:

- **An epic wins over a backlog.** Naming an `epicId` on a task also files it
  in that epic's backlog, whatever `backlogId` the same request carried.
  Moving a task to a different backlog without naming an epic clears its
  epic, and moving an *epic* between backlogs moves its tasks with it.
- **An epic belongs to one project.** An epic in another project's backlog is
  rejected with 400 `invalid_backlog`.

Deleting an epic never deletes its tasks: they drop back to sitting directly
in their backlog, exactly where they were before the epic existed. Abandoning
the rung costs nothing.

See [ADR-0012](docs/decisions/0012-why-an-epic-layer.md) for why this is a
separate object rather than a parent task, and why it stays out of GitLab.

### An epic's base branch and change scope

This is what the rung mainly exists for. A backlog is often too coarse to
name one branch: two coarse units inside it routinely target different
release branches, and the only way to say so used to be splitting the
backlog — which loses the grouping the backlog was there to provide.

An epic therefore carries its own `baseBranch`, `allowedScope` and
`forbiddenScope`, with the same validation the backlog's have. They are
resolved into `GET /api/v1/tasks/{taskID}/context` **epic first, then
backlog, per field**:

| Epic | Backlog | Task context reads |
| --- | --- | --- |
| `release/2.4` | `main` | `release/2.4` |
| `""` | `main` | `main` |
| `""` | `""` | `""` |

Per field, not per object: an epic that overrides only `baseBranch` still
inherits its backlog's scope. The Epic single view says which of the two each
value came from, since only one of them follows the backlog when it changes.

The issue destination for a *new* task gains the same rung: the task's epic's
link, then its backlog's, then the project's default, then nowhere at all.
Like the rungs below it, that is read **only when the task is created** —
`task_gitlab_links` governs every later update, so moving a task between
epics never moves or re-targets an issue that already exists.

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
  "progress": "in_progress",    // "not_started" | "in_progress" | "on_hold" | "done"
  "progressGuidance": "Update this task's progress as you work, via PATCH …",
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
a 403, so a token can't distinguish "not yours" from "does not exist". All
four fields are `""`, never `null`, until set: `acceptanceCriteria`/
`aiContext` via `PUT /api/v1/tasks/{taskID}/ai-context`, `allowedScope`/
`forbiddenScope` (and `baseBranch`) via the task's epic and then its backlog,
resolved per field (see "An epic's base branch and change scope" above) —
`""` either way when neither sets one.

To list several tasks at once (e.g. an agent polling its queue), use
`GET /api/v1/projects/{projectID}/tasks/context?status=open&per_page=20`,
which returns the same per-task shape plus `nextPage` (`0` when there is no
next page). `?updated_since=<RFC 3339 timestamp>` filters to tasks touched
at or after it, for incremental polling.

#### Progress convention for agents (issue #170)

FlowLens can only measure how long work actually takes if an agent keeps
`progress` current while it works — `PATCH /api/v1/tasks/{taskID}` with
`{"progress": "..."}`, the same `write`-scoped route as any other task edit:

- `in_progress` — set it the moment the agent starts working the task.
- `on_hold` — set it whenever the agent is blocked or waiting on something
  outside the task (a human review, a flaky CI run, a dependency). This is
  the value the eventual bottleneck detection leans on hardest: leave it
  unused and wait time silently gets counted as work time.
- `done` — set it once the work is finished.

Never PATCH `status`; that field mirrors the GitLab issue's open/closed
state and is kept in sync automatically in both directions, independently
of `progress` (see "Task & backlog progress" below).

Every `PATCH` that changes `progress` is recorded as a
`task_progress_events` row (issue #169), attributed to `"agent"` when the
caller is a bearer token — that log is what
[Flow metrics](#flow-metrics-issue-171) reads lead time and wait time off
of, so an agent that never calls `PATCH` leaves nothing to measure. This
convention is not only written down
here: the `progressGuidance` field above carries the same instructions in
every `GET .../context` response, since that response is the one thing an
agent working through a token reliably reads, unlike this README.

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
| POST | `/projects/{projectID}/tasks/bulk` | `write` |
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

### Agent Kit: setting up an AI agent in the repo it works in (issue #203)

An OpenAPI spec (served at `GET /openapi.yaml`, see above) resolves *what
schema* an endpoint takes, but not *what order* to call things in — that's
what `@motokis-lab/agent-kit` installs into the repository an AI agent
actually works in, as a Claude Code skill and three slash commands.

```bash
export FLOWLENS_API_TOKEN=flt_...   # optional, see below
npx @motokis-lab/agent-kit init --url https://flowlens.internal
```

This is deliberately not an npm dependency of the consumer repo: FlowLens
tracks repos in any language, and a self-hosted instance's version drifts
from whatever a published package would pin, which would eventually point
an agent at endpoints that don't exist on that instance. `npx` needs no
`package.json` in the target repo, and `init` re-fetches the spec from the
connected instance itself rather than bundling one.

There is no `--project` flag: a project API token (see "API tokens" above)
is already scoped to exactly one project, so `init` resolves it itself via
`GET /api/v1/projects`, which returns only the token's own project for a
bearer-authenticated request — never every project its owner has.

`init` writes:

| Path | Contents | Committed? |
| --- | --- | --- |
| `.claude/skills/flowlens/SKILL.md` | Auth, the task lifecycle's call order, branch-naming rule | yes |
| `.claude/commands/flowlens/refine-backlog.md` | `/flowlens:refine-backlog` — turn a backlog into numbered requirements | yes |
| `.claude/commands/flowlens/breakdown.md` | `/flowlens:breakdown` — split a backlog into sized, scoped, dependency-ordered tasks via [bulk task creation](#bulk-task-creation) | yes |
| `.claude/commands/flowlens/work.md` | `/flowlens:work` — run one task's design → implementation lifecycle | yes |
| `.flowlens/openapi.yaml` | The connected instance's OpenAPI spec | yes |
| `.flowlens/config.json` | `baseUrl` / `projectId` | yes |

The skill and commands need no live FlowLens instance and are always
installed (skipped if already present, unless `--force`) — they need to
reach a cloned-but-not-run colleague and a CI-triggered agent, not just
whoever ran `init`. `.flowlens/` mirrors the connected instance instead,
so it's only written when `FLOWLENS_API_TOKEN` is set **and** `--url` is
reachable; `init` resolves the project through that token, fetches the
spec, and writes both files, refreshing them on every subsequent run
(`--force` or not) since they change on every FlowLens release. When
either the token or the instance is missing, `init` still installs the
skill/commands, only warns, and leaves `.flowlens/` for a later run.
Both are committed, unlike the previous gitignored versions, so a
colleague or CI agent who just clones the repo has them without running
`init` themselves.

Commands live under the `.claude/commands/flowlens/` directory namespace
(`/flowlens:breakdown`, not a flat `/breakdown`), so they can't collide
with a repo's own commands and uninstalling is just deleting the
directory. The skill assumes the same project API token is reachable
through `FLOWLENS_API_TOKEN` at call time — `init` never stores it — and
that only one `/flowlens:work` runs at a time: there is no task-claiming
endpoint yet, so concurrent agents can race onto the same task.

### Changing your password

Every account can change its own password from **Settings → Password**, or
directly against the API. The route is session-only: a project API token
can never call it, since a token must not be able to take over the account
it acts as (see [What a token can't reach](#what-a-token-cant-reach)
above).

```bash
curl -X PUT "$API_BASE_URL/api/v1/me/password" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -d '{"currentPassword": "…", "newPassword": "…"}'
```

A successful change (`204`) **revokes every session the user holds** and
issues a fresh one in the same response. Changing a password is what
someone does when they think a session of theirs is in the wrong hands, so
no older token survives it — including the one that made the call, which is
replaced rather than kept, so the caller stays signed in.

There is no password-reset email flow: FlowLens has no mail transport and
targets closed networks. An account whose password is lost is recovered by
an operator, with `flowlens-api hash-password` — see
[Recovering a lost password](docs/self-hosting.md#recovering-a-lost-password).

### Project membership

A project can have more than one user, each with a role — `owner`,
`member`, or `viewer` — recorded in `project_members`
([ADR-0010](docs/decisions/0010-why-project-membership.md)). Managing
membership is always owner-only, session-only (a project API token can
never reach these routes — see
[What a token can't reach](#what-a-token-cant-reach) above).

Adding a member this way resolves an **existing** FlowLens account by
username or email. For someone who has no account yet — the normal case on
an instance with `ALLOW_SIGNUP=false` — use an
[invite link](#invite-links) instead.

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
    "isProjectOwner": false,
    "createdAt": "2026-08-02T00:00:00Z"
  }
]
```

The response never includes email — this endpoint accepts a username or
email to invite, but returning one back would let an owner use it to
enumerate registered accounts. `isProjectOwner` marks the row belonging to
the project's single designated owner (`projects.owner_user_id`); `role`
alone cannot identify them, since any number of members may hold the
`owner` role.

To find someone without knowing their exact identifier, the invite form
searches candidates as you type:

```bash
# Owner-only, session-only; q shorter than 2 characters returns []
curl "$API_BASE_URL/api/v1/projects/$PROJECT_ID/member-candidates?q=octo" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE"
```

```jsonc
// GET /api/v1/projects/{projectID}/member-candidates?q=octo
[{ "userId": "b7e1...", "username": "octocat", "displayName": "Octo Cat" }]
```

The candidate set is deliberately narrow: only users the caller **already
shares some project with**, minus the caller and minus this project's
existing members, capped at 10 hits. Email is neither matched nor returned.
FlowLens has no tenant boundary, so a general user-search endpoint would
hand every signed-up account a directory of every other account — the same
enumeration risk that keeps email out of the member list. Anyone outside
that set can still be invited through `POST .../members`, which is
unchanged: you just have to know their exact username or email.

Three invariants apply to `PATCH`/`DELETE .../members/{userID}`, all
returning `400`: you cannot change your own role, you cannot remove
yourself, and the designated owner can neither be demoted nor removed.
These routes manage *other* people's access — without the self-removal
rule a co-owner could delete their own membership and lock themselves out
of a project they have no way back into. There is deliberately no "leave
project" action yet; if one is added it will be its own endpoint. The web
app applies the same rules ahead of time: in the Project view's Members
section, your own row and the designated owner's render as a plain role
badge with no controls.


### Invite links

An invite is a single-use link that lets someone with **no FlowLens account
at all** create one and join a project. It exists because the two halves of
onboarding otherwise contradict each other: adding a member needs the person
to be registered already, and [`docs/self-hosting.md`](docs/self-hosting.md)
tells you to close registration with `ALLOW_SIGNUP=false`. An invite reopens
that door for one named person, once, instead of reopening it for everyone.

From the project's single view, open the **Invites** card, click **Create
invite**, pick a role and an expiry, and copy the link — shown exactly once,
like an API token, because only its SHA-256 hash is stored. **FlowLens sends
no email**: it has no mail transport and targets closed networks, so you
hand the link over yourself.

```bash
# Owner-only, session-only.
curl -X POST "$API_BASE_URL/api/v1/projects/$PROJECT_ID/invites" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -d '{"role": "member", "expiresInDays": 7}'
```

```jsonc
{
  "id": "b7e1...",
  "projectId": "a1b2...",
  "role": "member",
  "tokenPrefix": "fli_9f3a2c1d",
  "status": "pending",
  "expiresAt": "2026-08-27T00:00:00Z",
  "createdAt": "2026-08-20T00:00:00Z",
  "token": "fli_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d" // only ever present here
}
```

The invitee opens `/invites/<token>`:

- **No account yet** — they get a sign-up form. That signup is exempt from
  `ALLOW_SIGNUP`, and creates the account *and* the membership together.
- **Already signed in** — they get a "Join project" button
  (`POST /api/v1/invites/accept`).

Whichever path, the invite is spent: a link admits exactly one person, and
`role` decides what they get. `GET .../invites` lists them (including the
accepted and expired ones, so you can see who was let in) and
`DELETE /api/v1/invites/{inviteID}` revokes one.

Everything that can go wrong on the acceptance path — unknown token,
expired, already used — is reported identically, so whoever holds a link
cannot probe which invites ever existed.
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
"Timeline" view mode (alongside "List" and the default "Board" mode, per the OOUI rule that
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
- That column belongs to the name: the title has the line to itself, and only a
  **high or urgent** priority appears under it, as the same badge the list rows
  use. Every priority and a task's progress are stated on the bar's tooltip
  instead — they are what the Board and List modes are read for, and as two pills
  *beside* the title they left it a few dozen pixels, so every row read as an
  ellipsis.
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
rather than appearing complete. Unlike the Task collection's Timeline, none of
this reads a task list: `GET .../backlogs` (issue #144) returns each backlog's
own `taskCount`/`closedTaskCount`, computed by a `LEFT JOIN` aggregate in the
same query, so the List row count, the Board card's ratio and the Timeline
bar's fill all come straight off the backlog object the collection already
fetched — the screen never has to fetch every task in the project just to
derive them. The backlog's single view is the exception: it already fetches
that one backlog's own tasks to list them, and derives its completion ratio
from that list instead, so a failed fetch there still reports the ratio as
unavailable rather than 0%.

The date math lives in `apps/web/lib/timeline.ts`, separate from the components
so it is unit-testable without rendering a chart; the zoom level and scroll
position are owned by the `useTimelineViewport` hook beside it, shared by both
timelines.

### Task collection search, filters and sort

The project-scoped Task collection (`/projects/{projectId}/tasks`) narrows and
orders its List and Timeline view modes together, since both are presentations
of the same filtered set (`docs/ui-design.md` rule 5):

- A free-text box (`TaskSearchBox`, debounced 300ms and shared with the
  cross-project collection's own box below) matches a task's title or
  description.
- The status filter (All / Open / Closed) defaults to **Open**, so closed
  tasks don't fill the list; the backlog filter (unchanged) narrows further,
  and a progress filter narrows by FlowLens's own work state.
- Sort offers **Manual** (the API's own `position` order — the default),
  **Due date**, **Priority**, **Progress** and **Recently updated**, the same
  four non-manual values the cross-project Task collection's `?sort=` accepts
  (below), so the two screens agree on what each one means. Sorting is a
  display order only; it never rewrites `position`.
- **The API applies all of it** (issue #143), not the browser: the filters
  are held in the URL (`?q=`, `?status=`, `?progress=`, `?sort=`, alongside
  the existing `?backlog=`), and changing one pushes a new query string that
  the server component turns into `GET
  /api/v1/projects/{projectID}/tasks?…` — the same round trip the
  cross-project collection makes. A reload, a shared link and the browser's
  back button all land on the same filtered list, and List, Board and
  Timeline are three presentations of that one response rather than three
  filters over a full one. Note this makes `?q=` the API's full-text match
  (below) rather than a substring match: "logi" no longer finds "login".
- There is deliberately **no pagination**: the matching tasks come back
  whole, which is what lets the List view group them by backlog and lets a
  bucket be reordered by drag-and-drop, since `PATCH .../tasks/order` wants
  that bucket's entire task ID list. Capping the response is the follow-up
  to make if a project ever grows past what one response should carry.

The Backlog collection (`/projects/{projectId}/backlogs`, issue #151) carries
the same idea across its own three view modes, at a smaller scale:

- A priority filter, a progress filter and a sort (**Manual**, **Due date**,
  **Priority**, **Progress**) sit in the same `CardHeader` shape as the Task
  collection's own row. Priority and progress are held in the URL and applied
  server-side (`?priority=`, `?progress=`, `?sort=priority|progress` on `GET
  .../backlogs`, above); `?sort=dueOn` has no server-side equivalent — a
  backlog's schedule is app-only — so `BacklogListSection` sorts that case
  itself, dueOn ascending with undated backlogs last.
- The name search box (also `TaskSearchBox`, parameterized with `label`) has
  no API support at all and is matched entirely client-side: a project's
  backlogs are already fetched in full for the List/Board/Timeline views, and
  run orders of magnitude fewer than tasks, so there's nothing to gain from a
  server round trip for it. It's still held in the URL (`?q=`) for
  shareability and reload, the same as the client-only filters above.

### Task full-text search

`GET /api/v1/projects/{projectID}/tasks` and the cross-project
`GET /api/v1/tasks` both also accept `?q=`, matching a task's title or
description, combinable with every other filter (`priority=`, `progress=`,
`status=`, etc.) and with `sort=`. It is backed by `tasks.search_vector`, a
`STORED` generated column (`to_tsvector('simple', title || ' ' ||
description)`) with a GIN index, so filtering happens in the database rather
than by fetching every task and matching client-side. The `'simple'` text
search configuration does no stemming or dictionary-based word segmentation —
deliberately, to avoid the extra dependency a real Japanese tokenizer
(pg_bigm/pgroonga) would add — so a query matches as long as it lines up with
a whole run of characters the parser tokenizes as one lexeme; there is no
"contains this substring anywhere" guarantee for CJK text the way there is
for space-separated words. Both Task collection screens' search boxes are
this same match: the project-scoped one stopped matching substrings
client-side when its filtering moved to the API (issue #143, above).

### Markdown descriptions

A task's and a backlog's `description`, a project's `description` and a task
comment's body are all stored as plain text and **rendered as GitHub-flavoured
Markdown** in the web app — headings, lists, task lists, tables, blockquotes,
fenced code and links. A bare URL pasted into any of them (`https://…`,
`www.…`) becomes a clickable link on its own, with no `[text](url)` needed.

This is a rendering change only: nothing about the stored value or the API
changed, and a description that contains no Markdown syntax reads exactly as
it always did. It also lines the web app up with GitLab, whose issue
descriptions — the other end of the two-way sync — have always been Markdown.

Two deliberate limits:

- **Raw HTML in a description is shown as text, not rendered.** A description
  can arrive from a GitLab issue written by anyone with access to that
  project, so the renderer (`react-markdown` + `remark-gfm`) builds a React
  tree instead of setting `innerHTML`, and raw HTML is dropped rather than
  sanitized. Links are restricted to `http`, `https` and `mailto`.
- **Images are shown as their alt text rather than fetched.** GitLab stores an
  issue's attachments as paths relative to its own project (`/uploads/…`),
  which FlowLens cannot resolve, so rendering them would reliably produce a
  broken image.

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
component for both tasks and backlogs, in list rows and the task single view.
On the timeline the badge sits under the title rather than beside it, and only
for `high`/`urgent`; the bar's tooltip states every priority — see
[the Gantt charts above](#task--backlog-scheduling-gantt-charts). A backlog's priority is independent of its tasks':
creating or editing one never reads or writes the other. Priority is no longer
the board's axis — see [Task & backlog progress](#task--backlog-progress)
below — but it stays a badge on every board card.

### Task & backlog assignee

A task and a backlog each carry an `assigneeUserId` — the **FlowLens project
member who owns the work**, drawn from `project_members`. It is optional
everywhere: nullable in the schema, never in a `required` request field, and
every object starts unassigned.

This is deliberately a second axis alongside a task's existing
`assigneeGitlabUserId`, which mirrors the GitLab issue's own assignee and
syncs both ways. The two are connected by a **one-way bridge**:

- Setting `assigneeUserId` also sets the GitLab assignee, when that member has
  a GitLab identity registered for the project's connection
  (`user_gitlab_identities`, see [Linking your GitLab account](#linking-your-gitlab-account)).
  That is what puts the assignment on the issue.
- A member with no registered identity is still a perfectly good assignee —
  the task is simply assigned inside FlowLens only, and the GitLab assignee is
  cleared rather than left pointing at someone else.
- **A GitLab-side assignee change never writes back to `assigneeUserId`.** The
  FlowLens assignee records what a human decided, so an inbound sync cannot
  silently reassign the work.
- An explicit `assigneeGitlabUserId` in the same request always wins over the
  bridge, which is how assigning a GitLab user who has no FlowLens account
  keeps working.

A backlog's assignee has no bridge at all: a backlog has no GitLab
counterpart, so it is app-only end to end, like `baseBranch` and
`allowedScope`.

- `POST`/`PATCH` on `/api/v1/projects/{projectID}/tasks` /
  `/api/v1/tasks/{taskID}`, `/api/v1/projects/{projectID}/backlogs` /
  `/api/v1/backlogs/{backlogID}`, and `POST .../tasks/bulk` all accept
  `assigneeUserId`. It follows the same partial-update contract as the rest of
  the object: a body without the key leaves both axes alone — which is what
  stops a PATCH of some unrelated field from reassigning the GitLab issue as a
  side effect — and an explicit `null` unassigns. A user who is not a member of
  the project is rejected with 400 `invalid_assignee`.
- `GET .../tasks`, `GET /api/v1/tasks` (cross-project) and `GET .../backlogs`
  accept `?assignee=`, which takes **`me`, a user UUID, or `unassigned`**.
  Previously it took only `me`; a UUID is what lets a lead see what someone
  else is carrying. For a task the filter matches on *either* axis — the
  FlowLens assignee or that same user's GitLab identity — so a task synced in
  from GitLab and a purely local one both show up for the same person.
  `unassigned` is the complement: assigned to nobody on either axis. Over a
  bearer token, `me` resolves to the token's project owner, since a token acts
  as that owner everywhere else in the API ([ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md)).
- `GET /api/v1/tasks/{taskID}/context` carries the assignee too, so an AI agent
  reading its context knows whether the work is already someone's, and whose.

`assigneeUsername`/`assigneeDisplayName` are resolved from `users` on read
rather than stored, so a rename is picked up without a backfill; both are `""`
when unassigned. Deleting a user unassigns their work rather than deleting it
(`ON DELETE SET NULL`).

The 000031 migration backfills `assignee_user_id` for tasks already assigned to
a GitLab user who is both a project member and has that identity registered —
without it every pre-existing task would read as unassigned. Skipping the
backfill loses nothing but display, since the `?assignee=` filter ORs the two
axes anyway.

### Task size

A task carries a `size` — one of `xs`, `s`, `m`, `l`, `xl`, defaulting to
`m`, the exact middle of the five — a coarse estimate of how much work it is.
Its purpose is to weight [Velocity](#velocity-issue-195): a raw completed-task
count can be inflated for free by splitting tasks smaller, and size is what
lets throughput measure the work finished rather than merely the items
finished. Like `priority`, it is **app-only and never synced to GitLab**
(GitLab CE issues have no size field; weight is an EE feature), and an issue
imported from GitLab starts at `m` for a human to size afterwards.

**This is deliberately not a story-point field.** Issue #195 rejected
estimates on the grounds that they rot when a human has to re-enter a number
on every task, and that reasoning still stands: `size` is a five-value
T-shirt scale, and the numeric weights it maps to —
`xs`=1, `s`=2, `m`=3, `l`=5, `xl`=8 — live in
`apps/api/internal/velocity`, not in the schema and not in anyone's typing.
The steps are Fibonacci-ish rather than linear because uncertainty grows
faster than size does. There is still no sprint/timebox concept, and no
"points per sprint" figure.

**Backlogs deliberately have no size**, unlike `priority` and `progress`
which both objects carry: a backlog's priority is genuinely independent of
its tasks', but its size would just be the sum of theirs, and a hand-entered
one could only ever contradict them.

- `POST`/`PATCH` on `/api/v1/projects/{projectID}/tasks` and
  `/api/v1/tasks/{taskID}` accept `size`, under the same partial-update
  contract as the rest of the task: a body without `size` leaves the stored
  value alone, while an absent `size` on create or an explicit empty string
  on either resets it to `m`. Any other value is rejected with 400
  `invalid_size`.
- `GET .../tasks` and `GET /api/v1/tasks` accept `?size=xs|s|m|l|xl` to
  narrow the list and `?sort=size` to order biggest-first (`xl` → `xs`),
  falling back to the manual position order to break ties — the same shape
  `?priority=`/`?sort=priority` already has, and equally independent of the
  drag-reorder `position` field.
- `GET /api/v1/tasks/{taskID}/context` reports `size`, so an agent picking up
  a task knows how large the work is expected to be before it starts.

In the web app, size is a select on the task single view's edit form
(labelled with its point weight, e.g. "L (5 pts)"), a badge on task rows and
the task single view, and a filter plus a sort option on the task collection.
The badge is deliberately neutral at every size rather than escalating in
colour the way priority does: size is not urgency, and a red XL would read as
a problem when it only means "this is big".

**Every task predating this feature reads as `m`.** That cannot be
backfilled, and it means points-based velocity is exactly 3x the task count
until sizes are actually set — see the `sizedTaskRatio` field and the note
the Velocity card shows on its Points tab.

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

**Progress sync on issue close is the one, opt-in exception** (issue #202).
A spec-driven flow ends with a merge, not a manual progress edit, so left as
is a merged task sits at whatever progress it had until a human notices —
which the Board, dashboard and velocity all read from. A project can turn on
a per-project setting so that closing a task's linked GitLab issue *also*
moves its progress to `done`:

- `PUT`/`GET /api/v1/projects/{projectID}/progress-sync-settings` (owner-only,
  `{enabled}`, off by default) — the same owner-only, always-exists-with-a-
  default shape as [notification settings](#notification-digest-issue-109)
  below.
- The write only ever moves progress *to* `done`, and only once: it fires on
  a genuine `open`→`closed` transition of the task's status (never on a
  redelivered or re-applied "already closed" update, so a duplicate webhook
  never appends a second event), and never if progress is already `done`.
  Reopening the issue (`closed`→`open`) never reverts it — the sync is
  one-directional. If a human has since moved progress off `done`, a stale
  re-apply of the same close will not put it back, since the transition
  already happened once.
  Both inbound paths apply it — a live `Issue Hook` webhook
  (`internal/webhookapply`) and the periodic `project.resync`/`project.import`
  walk (`internal/projectsync`) — and both write it atomically with the
  status change that triggered it, so a crash between the two never leaves
  one without the other.
- The change is recorded as a `task_progress_events` row with
  `actor_kind = "gitlab"` (`internal/task`'s `ActorKindGitlab`), the third
  value alongside `"user"`/`"agent"` (issue #169) — an audit trail entry with
  no acting user (`actor_user_id` is `NULL`), the same as `"agent"`.
  [Velocity](#velocity-issue-195)'s user/agent/unknown actor split does not
  yet have its own `"gitlab"` bucket and currently counts these under
  "unknown" — left as a known gap, not implemented as part of issue #202.

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
in list rows and the single views. On the timeline it is stated on the bar's
tooltip rather than in the name column, for the same reason as priority above.
A backlog's progress
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
- **Task board** (`/projects/{projectId}/tasks`, the *default* mode here too):
  each card names the task's backlog (or Unclassified), its due date
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
drag would otherwise fight the display order it's shown in. The Backlog
collection (issue #151) hides them under a wider condition — any priority/
progress filter or name search active, not just a non-manual sort — because
unlike a task's per-backlog bucket order, a backlog's order is project-wide
and `PATCH .../backlogs/order` requires *every* current backlog in the
request; a filtered or searched list is, by construction, less than that full
set, and dragging within it would only produce a guaranteed
`backlog_ids_mismatch`. The drag-and-drop itself is native HTML5
drag-and-drop, not a dedicated library — no new frontend dependency was
available to add when this shipped; swapping in a library like `@dnd-kit`
later is a UI-only change, since it would still call the same `.../order`
endpoints.

### Bulk task creation

A spec-driven breakdown of a backlog typically produces 10-30 tasks and the
dependencies between them at once (issue #201). Creating those one `POST
/tasks` at a time is unsafe for that: each call creates a GitLab issue in the
same transaction as the task write (see "GitLab CE connection & sync"
above), so a failure partway through a manual loop leaves a half-decomposed
backlog with no way to unwind the issues already created, and dependencies
can't be wired up at all until every task's ID exists.

`POST /api/v1/projects/{projectID}/tasks/bulk` creates a whole batch of tasks
and the dependencies between them in one request and one transaction:
either every task, every dependency and every `issue.create` outbox job
commits, or none of it does. Requires write scope for a bearer token, the
same as `POST /tasks`.

- Request body: `{tasks: [...], dependencies: [...]}`. Each task carries a
  `ref` — a temporary ID valid only within that request, since the tasks
  don't have real IDs yet — plus the same fields `POST /tasks` accepts
  (`title` required; `description`, `backlogId`, `labels`, `dueOn`,
  `startDate`, `priority`, `size`) and an optional inline `aiContext`
  (`acceptanceCriteria`, `aiContext`, upserted alongside the task). Up to
  100 tasks per request.
- Each dependency is `{predecessorRef, successorRef}`, naming two `ref`s
  from the same request's `tasks` — a bulk dependency can only connect two
  tasks created in the same batch, not an existing task by ID. The
  single-dependency endpoint (`POST /task-dependencies`) already covers
  existing-to-existing edges; wiring together the batch just created is
  bulk's whole purpose, so that's the only case it supports.
- Every task and dependency is validated before anything is written — an
  empty or duplicate `ref`, an invalid field, a `ref` a dependency doesn't
  recognize, a self-dependency, or a dependency that would create a cycle
  (checked against the new batch's own edges, the same reachability check
  `POST /task-dependencies` uses) all fail the whole request with 400 and
  name the offending `ref` in the error message. Nothing is written on any
  failure.
- Response: `{tasks: [{ref, task}], dependencies: [...]}`, so a caller can
  resolve its own `ref`s to the tasks' real IDs.

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
  due date last), `assignee=me` (only tasks assigned to the caller's own
  registered GitLab identity — see [GitLab user identity](#gitlab-user-identity)
  below; a caller with no registered identity gets an empty list, not an
  error), and `q=` (free-text over title/description — see
  [Task full-text search](#task-full-text-search) above). `limit=` caps the
  result count (default 50, max 200); there is no cursor/offset pagination
  yet. The same `assignee=me` and `q=` filters are also accepted on the
  per-project `GET .../tasks` list.
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
canonical single view under its own project. Its search box is debounced and
held in `?q=`, the same as its other filters: typing round-trips to `GET
/api/v1/tasks?q=`, exactly as the project-scoped Task collection's own box
does (see [Task collection search, filters and
sort](#task-collection-search-filters-and-sort) above). `?progress=` and
`?sort=progress` are accepted here too, deep-linkable though the screen has
no progress control of its own.

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

### Notification digest (issue #109)

Overdue tasks and sync failures used to require an actual `/dashboard`
visit to notice. A background worker now sends a daily digest per project by
**outgoing webhook** — chosen over email because it needs no SMTP setup and
plugs straight into Slack via an Incoming Webhook URL (agreed on the issue
before implementation).

- `PUT`/`GET /api/v1/projects/{projectID}/notification-settings` (session,
  owner-only — the webhook URL is an outbound destination a lesser role
  should not be able to redirect, the same reasoning `gitlab-connection`
  applies to its credential): `{ "webhookUrl": string, "enabled": boolean,
  "sendHour": 0-23 }`. `GET` on a project that has never configured
  notifications returns the unconfigured defaults (`enabled: false`,
  `sendHour: 9`) rather than 404 — settings conceptually always exist.
- A background worker (`internal/notification.Worker`, same process as the
  sync workers, gated by `SYNC_WORKER_ENABLED`) sweeps every enabled project
  roughly every 15 minutes. Once a project's `sendHour` (UTC) has been
  reached, it builds that project's digest: open tasks overdue, open tasks
  due today (the finest-grained "within 24h" `due_on`'s `DATE` type
  supports), and failed `sync_jobs` / `webhook_events`. **A digest with
  nothing to report is never sent.**
- Sending is logged to `notification_digests` (`project_id`, `digest_date`)
  *before* the webhook POST, and that table's `UNIQUE (project_id,
  digest_date)` constraint is the whole dedupe guard: a second sweep the
  same day (or a second process) hits the constraint and skips instead of
  double-sending. A day with nothing to report never rows there at all, so
  it doesn't block a later same-day sweep once something *does* need
  reporting.

### Merge request sync (issue #111)

Read-only sync of a linked GitLab project's merge requests and their latest
pipeline status, building on [ADR-0011](docs/decisions/0011-why-merge-request-sync.md)'s
schema/design. **FlowLens never writes a merge request back to GitLab** —
unlike issue sync, this is one-way.

- A `repositories` row (the MR-tracking sibling of a `linked_gitlab_projects`
  row) and an initial import are both created automatically the moment a
  GitLab project is linked, alongside issue sync's own initial import — no
  separate "enable MR sync" step.
- **Webhook-primary:** linking a project's webhook now also requests
  `merge_requests`/`pipeline` events (previously issues/notes only).
  `Merge Request Hook` deliveries create/update a `merge_requests` row;
  `Pipeline Hook` deliveries update the merge request's
  `pipeline_status`/`pipeline_id`/`pipeline_updated_at` when the pipeline
  names a merge request (`merge_request.iid`) already imported — a plain
  branch/tag pipeline, or one for an MR not yet imported, is skipped.
  Idempotency is the same `gitlab_merge_request_id` UNIQUE constraint /
  strict `updated_at` staleness guard issue sync uses.
- **Periodic catch-up:** `internal/mrsync` walks every page of a
  repository's merge requests (`mr.import` sync job, the same outbox/worker
  shape as `project.import`), fetching each one's current pipeline
  (`head_pipeline`) and, once, its first review activity (earliest note from
  someone other than the author, `first_reviewed_at` — GitLab CE's
  approvals endpoint carries no per-approval timestamp, so notes are the
  only source with one) and recording the run on `repository_sync_runs`.
- **Task linking:** a merge request whose description contains a closing
  keyword (`Closes #12`, `fixes #12`, ...) or whose source branch starts
  with an issue number (`12-fix-thing`, `issue-12`) is linked to that
  issue's task via the existing `task_gitlab_links` table, giving a
  task → MR → pipeline chain. A merge request that references nothing
  recognizable is simply left unlinked.
- See [Merge request views](#merge-request-views-issue-112) for the API/UI
  that surfaces this data.

### Merge request views (issue #112)

The `MergeRequest` collection/single views the object model in
[`docs/ui-design.md`](docs/ui-design.md) has anticipated since before either
existed. Read-only throughout — FlowLens never writes a merge request back to
GitLab (ADR-0011), so unlike the `Task` screens there is no create/edit/delete
here.

- `GET /api/v1/projects/{projectID}/merge-requests` lists a project's merge
  requests, scoped through `repositories` → `linked_gitlab_projects` →
  `gitlab_connections` to the caller's project membership, the same
  `project_members` check the task collection uses. Filters: `?state=`
  (`opened`/`merged`/`closed`/`locked`), `?author=` (GitLab username),
  `?taskId=` (only the merge request(s) linked to one task), `?since=`/
  `?until=` (`YYYY-MM-DD`, bounding `gitlab_created_at`), `?sort=updated`
  (ranks by `gitlab_updated_at` instead of the default `gitlab_created_at`,
  both descending).
- That list is **paged**: `?page=` (1-based) and `?per_page=` (default 30,
  clamped to 100), and the response is the envelope
  `{ "mergeRequests": [...], "nextPage": 0, "totalCount": 0 }` rather than a
  bare array — `nextPage` is `0` when no further page follows, the same shape
  `GET .../webhook-events` returns, and `totalCount` is how many merge
  requests match the filter across every page. A repository synced for a year
  holds thousands of merged merge requests, and this endpoint used to return
  every one of them in a single response.
- `GET /api/v1/merge-requests/{mergeRequestID}` returns a single merge
  request, scoped the same way.
- Web: `/projects/[projectId]/merge-requests` (collection) and
  `/projects/[projectId]/merge-requests/[mrId]` (single, showing review/
  pipeline status and a link to its linked `Task` if any) — see the screen
  map in [`docs/ui-design.md`](docs/ui-design.md). The Task single view also
  shows a "Merge requests" card, the reverse link, via the same list endpoint
  filtered by `?taskId=`.
- The collection view opens on **open merge requests, most recently updated
  first** (`?state=` defaults to `opened`, `?sort=` to `updated`) rather than
  on the project's entire merge-request history. That is what the screen is
  for: seeing what is in flight, and drilling into the project's
  [delivery metrics](#delivery-metrics-issue-113) when a number moves. "All
  states" stays one click away, and `?state=all` is the explicit opt-out.
  Paging is held in the URL as `?page=`, so a deep page can be linked to and
  survives a refresh; changing any filter resets to page 1.
- Four indexes on `merge_requests` (migration 28) back the list's two sorts,
  each in a state-filtered and an unfiltered form. Each leads with the **sort
  key**, not `repository_id`: the project scope reaches this query through a
  join rather than a `WHERE` clause, so an index leading with `repository_id`
  is never chosen. Measured against 50k merge requests, that is 54ms → 0.16ms
  for "all states" and 15ms → 0.29ms for the view's default.

### Delivery metrics (issue #113)

The merge-request / CI delivery-flow metrics [ADR-0011](docs/decisions/0011-why-merge-request-sync.md)
designed and [#111](#merge-request-sync-issue-111)/[#112](#merge-request-views-issue-112)
populated, finally aggregated and charted — this is what makes FlowLens live
up to its name. Read-only, computed on request from `merge_requests` already
synced; nothing is cached or materialized yet.

- `GET /api/v1/projects/{projectID}/metrics?from=&to=` (`YYYY-MM-DD`, both
  optional, bounding `gitlab_created_at` the same way the merge-request
  collection's `?since=`/`?until=` do) returns:
  - **Open → first review** and **first review → merge** durations, each as
    a **median and p90** (never a mean — lead time is reliably skewed by a
    handful of slow reviews, and a mean lets outliers hide behind a falsely
    comfortable "average"). A merge request missing either timestamp (not
    yet reviewed, not yet merged) is excluded from that stat rather than
    counted as zero.
  - **Merge-request size distribution** (median/p90 of `additions`/
    `deletions`/`changed_files`) — the columns already exist on
    `merge_requests`, but `internal/mrsync` doesn't fetch GitLab's diff
    stats yet, so every merge request's size is 0 today; this aggregation is
    ready for when a future issue backfills them.
  - **Pipeline success rate**: `success ÷ (success + failed)` pipelines;
    `null` when nothing in range has a decided outcome (still
    running/pending/skipped/canceled/manual/no pipeline don't count toward
    either side).
  - **Throughput**: count of merge requests with state `merged` in range.
  - Session-only, not on the bearer-token allowlist — this is a chart for a
    human reading the Project view, not an AI-facing read.
  - Median/p90 use the nearest-rank method (no interpolation) — see
    `apps/api/internal/deliverymetrics`.
  - `&interval=week|month|year` (issue #188) additionally buckets the same
    stats into a `periods` time series, so "is this improving?" is readable
    alongside "how is it now?" — see [Period bucketing](#period-bucketing-issue-188)
    below; the rules there (cohort basis, UTC/ISO week, gap-fill, 52-period
    cap) apply identically here, bucketing by `merge_requests.gitlab_created_at`.
- Web: a stat row (throughput, pipeline success rate) on the "Delivery
  metrics" card on the Project single view (`/projects/[projectId]`), with
  `?from=`/`?to=` date filters held in the URL. The open→first-review/
  first-review→merge lead time is no longer charted on its own here — see
  [Flow metrics](#flow-metrics-issue-171)'s `reviewAndMerge` stage, which the
  same card now charts instead (issue #172). Size distribution isn't charted
  yet — see above. With `?interval=` selected (issue #189, an `All`/`Week`/
  `Month`/`Year` selector next to the date filters), the stat row gains a
  small throughput bar chart and pipeline-success-rate line chart underneath,
  one point per period.
- The aggregation started as a plain query over `merge_requests`, computing
  median/p90 in the application layer (cheap to unit test with fakes, per
  [`docs/testing.md`](docs/testing.md)); a materialized view is future work
  if `EXPLAIN` on real data ever calls for one, not before.

### Flow metrics (issue #171)

Once the `task_progress_events` log the
[progress convention for agents](#progress-convention-for-agents-issue-170)
populates has real history in it, it can be aggregated into per-stage lead
time — the work `internal/flowmetrics` does, in the same read-only,
compute-on-request shape as [Delivery metrics](#delivery-metrics-issue-113).
Where delivery metrics look only at `merge_requests`, flow metrics walk a
task's whole pipeline: from creation, through an agent picking it up,
through the merge request that closes it out, to the task being marked
done.

- `GET /api/v1/projects/{projectID}/flow-metrics?from=&to=&interval=` (`from`/
  `to` as `YYYY-MM-DD`, both optional, bounding `tasks.created_at`;
  `interval` covered in [Period bucketing](#period-bucketing-issue-188)
  below) returns six stages, each as a
  **median and p90** in hours (never a mean, same rationale as delivery
  metrics) over only the tasks that reached *both* ends of that stage — a
  task that hasn't reached a stage's end yet is excluded from it rather than
  counted as a zero duration:
  - **`waitingToStart`**: `tasks.created_at` → the task's first transition
    to `in_progress`.
  - **`design`**: `tasks.design_started_at` → `tasks.implementation_started_at`
    (migration `000023`). Unlike every other stage here, these two
    timestamps aren't derived from `task_progress_events` — they're written
    directly by whoever starts that phase (an AI agent doing spec-driven
    development, or a human) via `POST /api/v1/tasks/{taskID}/design-started`
    and `POST /api/v1/tasks/{taskID}/implementation-started`. Both endpoints
    are session- and bearer-token-writable (`write` scope), **always
    overwrite** — unlike most task fields there is no "already set" guard,
    so redoing the design after review feedback just moves the timestamp
    forward — and are independent of each other: a task with
    `implementation_started_at` but no `design_started_at` simply skipped
    the design phase and is excluded from `design` (not counted as zero)
    while still counting toward `implementation`. A task that never calls
    either endpoint has no `design`/`implementation` sample at all — this
    pair is opt-in, unlike every other stage, which needs no extra call.
  - **`implementation`**: `tasks.implementation_started_at` → the earliest
    linked merge request's `gitlab_created_at`. A task with more than one
    linked merge request (a follow-up MR, say) is measured against the
    earliest one, since that's the one that actually closed the wait.
  - **`reviewAndMerge`**: that merge request's `gitlab_created_at` →
    `merged_at` — one span, unlike delivery metrics' open→first-review/
    first-review→merge split; `first_reviewed_at` isn't used here.
  - **`completion`**: `merged_at` → the task's first transition to `done`.
  - **`blocked`**: cumulative time across every *closed* `on_hold`
    interval a task passed through (entering and later leaving, however
    many round trips); a task still `on_hold` with no exit yet has that
    stretch excluded, and a task never `on_hold` at all is excluded
    entirely rather than counted as zero.
  - A task with no linked merge request (no code change involved — a
    research spike, a docs task) is excluded from `implementation` and
    `reviewAndMerge` by the same "both ends known" rule. **This is
    intentional, not a bug**: those two stages measure code-review lead
    time, which doesn't exist for a task that never produced a merge
    request.
  - Session-only, not on the bearer-token allowlist — like delivery
    metrics, this is a chart for a human reading the Project view, not an
    AI-facing read.
- Median/p90 use the same nearest-rank method as delivery metrics —
  currently duplicated in `apps/api/internal/flowmetrics` rather than
  shared, since the two aggregations are still small and independent.
- Web (issue #172, narrowed to Design-onward by the `design`/`implementation`
  split above): the same "Delivery metrics" card on the Project single view
  charts `design`, `implementation`, `reviewAndMerge` and `completion` as a
  stacked horizontal bar — a value-stream map, so the tallest segment reads
  as the bottleneck at a glance. `waitingToStart` and the two backlog-level
  stages below are still returned by the API but are not part of this
  chart. `blocked` is charted separately from that stack, never folded into
  it, so blocked time is never double-counted against the stage it
  interrupted. Median and p90 switch via a shared tab above both charts
  (issue #189) rather than drawing two rows at once — one piece of state for
  both, since letting them switch independently would invite reading one
  chart's median against the other's p90. It shares the card's
  `?from=`/`?to=` filters with delivery metrics.

#### Backlog-level stages: waiting to start and task breakdown (issue #173)

A task's whole pipeline above still starts at `tasks.created_at` — it says
nothing about how long a backlog sat before anyone started it, or how long
it took to break that backlog down into tasks once someone did. `#169`'s
`task_progress_events` can't answer either question, since neither happens
to a task. `backlog_progress_events` (migration `000022`) is the
backlog-level counterpart, an append-only log of `backlogs.progress`
transitions in the same shape, written from the single insertion point
`internal/backlog.Service.Update` — only when `progress` actually changes,
attributed to `actor_kind`/`actor_user_id` exactly like a task's own
(`"agent"` for a bearer-token caller, `"user"` for a session caller, since
`PATCH /backlogs/{backlogID}` is on the same shared allowlist as a task's).

`GET /api/v1/projects/{projectID}/flow-metrics?from=&to=` returns two more
stages alongside the five above, this time bounding `backlogs.created_at`
rather than `tasks.created_at`:

- **`backlogWaitingToStart`**: `backlogs.created_at` → a backlog's first
  transition to `in_progress`.
- **`taskBreakdown`**: that same transition → the earliest `created_at`
  among the backlog's tasks — the AI-driven "break this backlog into tasks"
  step this flow means to measure. A backlog that already had a task filed
  under it *before* going `in_progress` is excluded from this stage
  entirely rather than counted as a zero (or negative) duration: there was
  no breakdown work left to time after the transition.

Both follow the same "both ends known, excluded rather than zero" rule as
every other stage. Both are returned by the API but, like `waitingToStart`,
are not part of the web stage-lead-time chart — that chart starts at
`design`, per the [design/implementation split](#flow-metrics-issue-171)
above. `blocked` is unaffected and stays its own separate chart.

#### Period bucketing (issue #188)

A single `?from=&to=` range says "how is it now?" but not "is it
improving?" — `&interval=week|month|year`, accepted by both
[Delivery metrics](#delivery-metrics-issue-113) and flow metrics above, adds
a `periods` time series to the response without changing any existing
field: **omitted, the response is byte-for-byte what it was before this
issue**, `"interval"` is `null`, and `"periods"` is empty.

- **Cohort basis, not event basis.** A period is chosen by when the row was
  *created*, not by when the value being measured happened — the same
  `?from=/?to=` bound each endpoint already filters by:
  - Flow metrics' task-level stages (`waitingToStart`/`design`/
    `implementation`/`reviewAndMerge`/`completion`/`blocked`): `tasks.created_at`.
  - Flow metrics' backlog-level stages (`backlogWaitingToStart`/
    `taskBreakdown`): `backlogs.created_at`.
  - Delivery metrics (all fields): `merge_requests.gitlab_created_at`.

  This means a period reports "how long did the cohort *created* in this
  window end up taking", not "what happened during this window" — the most
  recent periods are thinner on data because much of that cohort hasn't
  finished yet. That's accepted deliberately (see issue #188): it keeps
  every row in exactly one period and keeps each period's median/p90
  computed from a consistent, non-overlapping sample.
- **UTC, ISO week.** Every boundary is UTC, matching `timestamptz` storage
  and every other aggregation here. A `week` bucket starts Monday 00:00 UTC
  (so a Sunday timestamp belongs to the *previous* Monday's week); `month`
  starts the 1st 00:00 UTC; `year` starts Jan 1 00:00 UTC. `end` on every
  period is exclusive.
- **Gap-filled.** Every bucket between the covered range's start and end is
  present with `count: 0`, even ones with no data at all — so a chart can
  render a flat/empty period instead of skipping straight to the next one
  with data. The covered range is `from`→`to` when given; a missing bound
  falls back to the earliest/latest bucket actually observed in the
  response's own data.
- **Capped at 52 periods.** An unbounded `?interval=week` could otherwise
  return hundreds of rows. Once the covered range would exceed 52 buckets,
  only the newest 52 are returned and the response's top-level
  `"truncated"` is `true`.
- An unrecognized `interval` value is a 400, the same treatment as a
  malformed `from`/`to`.
- Shared boundary math (`BucketStart`/`BucketEnd`/the gap-fill+cap
  `Timeline`) lives in `apps/api/internal/metricsperiod`, used by both
  `internal/flowmetrics` and `internal/deliverymetrics` — bucket-boundary
  bugs are the kind worth fixing in one place, unlike the median/p90 helpers
  those two packages still duplicate.
- Web (issue #189): an `All`/`Week`/`Month`/`Year` selector next to the
  "Delivery metrics" card's date filters, held in the URL as `?interval=`
  alongside `?from=`/`?to=` (server-refetched, not client-recomputed —
  the same hand-off-through-the-URL pattern the date filters already use).
  With an interval selected:
  - Stage lead time and Blocked time each draw one horizontal stacked-bar row
    per period instead of one summary row, oldest period on top/newest on
    bottom — reading top-to-bottom shows whether lead time is shrinking. A
    period with `count: 0` still draws its (empty) row, so a gap in the data
    reads as a gap rather than silently disappearing. `"truncated": true`
    shows a one-line note that only the most recent 52 periods are shown.
  - The stat row gains a small throughput bar chart and pipeline-success-rate
    line chart underneath, one point per period.
  - `All` (the default, no `interval` in the URL) is unchanged from before
    this issue except that Stage/Blocked draw only the tab-selected stat's
    row, not both at once — see [Flow metrics](#flow-metrics-issue-171)'s
    Median/p90 tab above.

### Velocity (issue #195)

Throughput — completed tasks per period — as distinct from
[Delivery metrics](#delivery-metrics-issue-113) and
[Flow metrics](#flow-metrics-issue-171), which both measure how long one
item took, not how many finished in a window. It is reported two ways at
once: a raw completed-task count, and a total weighted by each task's
[size](#task-size). Both are split by `task_progress_events.actor_kind` into
user/agent/unknown, a breakdown no story-point tool can give, since "how
much throughput did the agent actually produce" only means something once
agents are doing the work.

The two units answer different questions and neither is redundant: a count
alone can be inflated for free by splitting tasks smaller, while points
alone hide whether the work arrived as a few large items or many small ones.
There is still deliberately no story-point/estimate concept — no sprint or
timebox for a "points per sprint" figure to hang off, and no number anyone
types per task; `size` is a five-value T-shirt scale and the weights
(`xs`=1 … `xl`=8) live in `internal/velocity`.

- A task's **completion time** is `min(its first progress='done'
  transition's occurred_at, tasks.closed_at)`, whichever is non-nil; a task
  with neither is not completed and is never counted. Both signals have to
  be checked: `tasks.progress` is app-only and GitLab sync does not write it
  by default, so a task closed on the GitLab side alone never reaches
  `progress='done'` and would be invisible if only `task_progress_events`
  were read — unless [progress sync on issue close](#task--backlog-progress)
  is turned on for the project, in which case that same GitLab-side close
  is exactly what writes `progress='done'`, via an `actor_kind = "gitlab"`
  event;
  conversely `tasks.status` can stay `open` after `progress` reaches
  `done` (they're separate axes that never write each other), so
  `closed_at` alone would miss those. Each task counts at most once, at
  the earlier of the two — a tie prefers the `done` transition, since it
  alone carries an actor. `task_progress_events` only exists from migration
  `000020` on (issue #169); a task done before that migration shipped has
  no event row and is only reachable via `closed_at`, with no actor
  breakdown — that gap can't be backfilled and is expected, not a bug.
- `GET /api/v1/projects/{projectID}/velocity?from=&to=&interval=` (`from`/
  `to` as `YYYY-MM-DD`, both optional) buckets tasks by their **completion
  time**, not their `created_at` — the opposite cohort basis from delivery/
  flow metrics above, since this endpoint answers "how much finished in
  this window", not "how is the cohort created in this window doing".
  `from`/`to` bound completion time the same way. `interval` follows
  [Period bucketing](#period-bucketing-issue-188)'s UTC/ISO-week
  boundaries, gap-fill and 52-period cap, but **defaults to `week`** when
  omitted rather than "don't bucket" — periods are the metric here, not an
  optional add-on. Session-only, not on the bearer-token allowlist, like
  the other two metrics endpoints. Each period reports:
  - `completed`, split into `completedByUser`/`completedByAgent`/
    `completedByUnknown` (always summing back to `completed`) —
    `completedByUnknown` covers a `closed_at`-only completion (no actor to
    read) and the pre-migration-000020 gap above.
  - `movingAverage`: the simple average of `completed` over this period and
    up to 3 preceding ones (fewer once fewer exist) — a single period's
    count is too noisy to act on alone; this is the value meant to
    actually be read.
  - `complete`: `false` for a still-running period (typically the most
    recent one), so a chart can tell a partial bucket apart from a
    finished one.
  - `completedPoints`, split the same three ways and by the same actor rule,
    weighting each completed task by its size.
  - The response also reports `openTaskCount` (current
    `status='open' AND progress<>'done'` count, regardless of `from`/`to`),
    `averageVelocity` (the mean `completed` over the most recent up to 4
    **complete** periods — excluding any still-running period, which would
    otherwise understate velocity by construction — `null` if none is
    complete yet), and `forecastPeriods` (`openTaskCount / averageVelocity`,
    `null` whenever that's `null` or `0`): how many more periods, at the
    recent pace, the remaining open tasks would take.
  - `openTaskPoints`, `averageVelocityPoints` and `forecastPeriodsByPoints`
    are the point-denominated counterparts of those three, by identical
    rules — `averageVelocityPoints` also excludes still-running periods.
    Once sizes are actually set, the point forecast is the more trustworthy
    of the two, since it accounts for the remaining work being unusually
    large or small instead of assuming an average-sized task.
  - `sizedTaskRatio` (0..1) is the fraction of the completed tasks counted
    whose size is something other than the default `m`. Every task predating
    the `size` column reads as `m`, so while this is `0` the point series is
    arithmetically 3x the count series and carries no extra information —
    the web card says so rather than presenting a rescaled duplicate as a
    second opinion.
- Web (issue #196): a "Velocity" card on the Project single view, placed
  immediately *before* the "Delivery metrics" card so velocity reads
  alongside lead time rather than as a screen of its own — there is
  deliberately no standalone `/velocity` screen, since throughput alone is
  easy to game (splitting tasks smaller inflates it for free) and only means
  something read next to lead time staying flat or improving. It shares
  "Delivery metrics"' `?from=&to=&interval=` URL filter rather than exposing
  a second selector; unlike that card, it always draws one bar per period
  (defaulting to `week` when `interval` is omitted, per the API default
  above).
  - A stacked bar per period: `completedByUser`/`completedByAgent`/
    `completedByUnknown`, in that order both in the stack and in the legend.
    `movingAverage` is overlaid as a line on the same chart, since a single
    period's bar is too noisy to read on its own.
  - A `Tasks`/`Points` tab switches bars, moving average and both stats
    together (never a mix) between the count and the size-weighted series.
    Both arrive pre-weighted from the API; the client never multiplies
    anything itself. On the Points tab, a project where no completed task has
    been sized yet gets a one-line note saying the series is just the task
    count x 3.
  - A still-running period (`complete: false`) draws its bars at reduced
    opacity, so a partial bucket is never misread as a slowdown.
  - `averageVelocity`/`forecastPeriods` are shown as a small stat row (e.g.
    "9.5 tasks/week" / "34 open ≈ 3.6 weeks left"); either being `null` shows
    a placeholder instead of a number.
  - A project with no completed tasks yet shows "No completed tasks yet."
    instead of an empty chart.

## API Reference

The Go API serves its own OpenAPI 3.1 document, unauthenticated, at
`GET $API_BASE_URL/openapi.yaml` and `GET $API_BASE_URL/openapi.json` —
every route in `internal/http`'s router, kept from drifting by a test that
walks the router and fails the build if it and `apps/api/openapi/` (the
document's source, bundled by `make generate` into the committed
`openapi.bundled.yaml`) disagree on the route set. It is unauthenticated
because this is an on-prem deployment where the API is already reachable
from any logged-in browser on the same origin — the document describes
route shapes, not secrets.

## Current limitations

- The token cipher is the local AES-GCM implementation; the Azure Key Vault
  implementation is not written yet (the interface is in place).
- Integration tests assume migrations are already applied.
- Merge-request size distribution is always 0 today: `internal/mrsync`
  doesn't fetch GitLab's diff stats (`additions`/`deletions`/
  `changed_files`) yet — see [Delivery metrics](#delivery-metrics-issue-113).

## Roadmap

1. **Foundation (done):** monorepo, Docker Compose, migrations, health
   check, local auth, sessions, `GET /api/v1/me`.
2. **Task tracker / GitLab CE issue sync (done):** projects, backlogs,
   tasks, per-project GitLab connection, linked GitLab projects,
   bidirectional sync, AI-facing context API — see
   [GitLab CE connection & sync](#gitlab-ce-connection--sync).
3. **Merge request sync design (done):** ADR + schema alignment — reuse the
   per-project GitLab connection, `repositories` hangs off
   `linked_gitlab_projects` instead of a dropped `organizations` table, and
   `merge_requests`/`merge_request_reviewers` carry GitLab vocabulary and the
   minimal delivery-flow metric columns — see
   [ADR-0011](docs/decisions/0011-why-merge-request-sync.md).
4. **Merge request sync (done):** webhook-primary (`merge_request`/
   `pipeline` events) with periodic catch-up, idempotent upserts, and
   task ↔ MR linking — see
   [Merge request sync](#merge-request-sync-issue-111).
5. **Merge request views (done):** collection view with filters, single
   view, task ↔ MR reverse link — see
   [Merge request views](#merge-request-views-issue-112).
6. **Delivery-flow dashboard (done):** aggregated metrics (review latency
   median/p90, pipeline success rate, throughput) across a project's merge
   requests, with empty/loading/error states — see
   [Delivery metrics](#delivery-metrics-issue-113).
7. **Progress transitions + flow metrics (done):** an append-only
   `task_progress_events` log of `progress` changes, the agent-facing
   convention for keeping it populated, and a stage-level lead-time
   aggregation (waiting-to-start/implementation/review-and-merge/
   completion/blocked) computed from it, plus a `backlog_progress_events`
   counterpart adding two more backlog-level stages (waiting-to-start/
   task-breakdown) one step earlier in the pipeline — see
   [Progress convention for agents](#progress-convention-for-agents-issue-170),
   [Flow metrics](#flow-metrics-issue-171), and
   [Backlog-level stages](#backlog-level-stages-waiting-to-start-and-task-breakdown-issue-173).
8. **Automation:** webhooks (with duplicate-delivery handling) and scheduled
   sync via Azure Service Bus.
9. **Self-hosting (done):** prebuilt multi-arch images on GHCR, a
   download-and-run `compose.yaml`, migrations applied by the API itself,
   and an upgrade path that is `pull` + restart — see
   [docs/self-hosting.md](docs/self-hosting.md).
10. **Managed deployment:** Azure Container Apps, Azure Database for
    PostgreSQL, Key Vault, Application Insights.
