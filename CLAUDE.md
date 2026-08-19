# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

FlowLens is a task tracker whose tasks are kept 1:1 with GitLab CE issues, plus a (longer-term) view of a team's software delivery process from GitLab CE MR/review/CI data. It does **not** generate or review code with AI — AI agents are a *consumer* of FlowLens data (via the task context API), not something FlowLens runs.

**Status:** Local username/password login (with signup), user persistence, and `GET /api/v1/me` are implemented. The task-tracker / GitLab CE Issue-sync MVP is also implemented: projects, backlogs, tasks, a per-project GitLab connection, linked GitLab projects, the outbox-backed sync worker, the webhook receiver, and the AI-facing task context API — see the "GitLab CE connection & sync" section in [`README.md`](README.md). Project-scoped API tokens are also implemented: an AI agent or other external integration reads and writes a project's tasks over `Authorization: Bearer <token>` instead of a session, scoped to `read`/`write` and to a fixed route allowlist — see the "API tokens" section in [`README.md`](README.md) and [ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md). A task's activity log (comments) is also implemented: `task_comments` records who said what — a human user, a project API token (`author_kind` "agent"), or in the future GitLab (`author_kind` "gitlab", reserved for the not-yet-built discussion sync) — via `GET`/`POST /api/v1/tasks/{taskID}/comments` and `DELETE /api/v1/task-comments/{commentID}` (own comment only), with the task's most recent comments also embedded in `GET /api/v1/tasks/{taskID}/context` — see the "Activity log (comments)" section in [`README.md`](README.md). Project membership is also implemented: a project can have more than one user, each with an `owner`/`member`/`viewer` role in `project_members`, with owner-only, session-only invite/list/role-change/remove endpoints — see the "Project membership" section in [`README.md`](README.md) and [ADR-0010](docs/decisions/0010-why-project-membership.md). Scheduling is also implemented: a task's `startDate` (alongside `dueOn`), predecessor/successor task dependencies (cycle-checked in the application layer), a backlog's own `startDate`/`dueOn`, and a "Timeline" (Gantt) view mode on both the Task and the Backlog collection — the backlog bars are filled by their tasks' closed/total ratio. See the "Task & backlog scheduling, Gantt charts" section in [`README.md`](README.md). A task and a backlog also each carry a `priority` (`low`/`medium`/`high`/`urgent`, defaulting to `medium`), filterable and sortable via `?priority=`/`?sort=priority` on their list endpoints — see the "Task & backlog priority" section in [`README.md`](README.md). They also each carry a `progress` (`not_started`/`in_progress`/`on_hold`/`done`, defaulting to `not_started`), FlowLens's own four-stage work state, filterable and sortable the same way via `?progress=`/`?sort=progress`; it is **not** a task's `status`, which stays the GitLab issue state (`open`/`closed`) and syncs both ways — neither value ever writes the other, with one opt-in exception (issue #202, see below). Progress is the axis of the "Board" view mode on both the Task and the Backlog collection (priority rides along as a badge) — see the "Task & backlog progress" section in [`README.md`](README.md). Both objects' manual `position` can also be bulk-reordered in one all-or-nothing request via `PATCH .../tasks/order` / `PATCH .../backlogs/order`, and the web app's Task/Backlog List views support reordering by drag-and-drop and by keyboard-accessible move-up/move-down buttons, with optimistic updates — see the "Task & backlog reordering" section in [`README.md`](README.md). A task's `startDate`, task dependencies, a backlog's dates and both objects' `priority`/`progress`/`position` are all app-only and never sync with GitLab. A backlog can also name its own `defaultLinkedGitlabProjectId`, overriding the project-wide default link: creating a task resolves its issue destination as backlog's link → project's default link → nowhere (a purely local task). That resolution runs **only at task-create time** — once an issue exists, `task_gitlab_links` is what every later update follows, so moving a task between backlogs never moves or re-targets the issue — and the named link must belong to the same project's GitLab connection (checked in `internal/backlog`, since the schema can't express it). See the "A backlog's own GitLab project" section in [`README.md`](README.md). A backlog can also name its own `baseBranch` — the branch its tasks are meant to branch from during development — via the same create/edit endpoints; optional, app-only, never synced to GitLab, and surfaced on `GET /api/v1/tasks/{taskID}/context` (resolved through the task's backlog) so an AI agent working a task knows what to branch from. See the "A backlog's base branch" section in [`README.md`](README.md). The cross-project Task collection (`GET /api/v1/tasks`, session-only) and its web screen at `/tasks` are also implemented — see the "Cross-project task collection" section in [`README.md`](README.md) — and `/dashboard`, the screen every login lands on, is built on that same endpoint plus `GET /api/v1/projects?failedSync=true`: overdue/due-soon/waiting-to-start/high-priority task teasers, a sync-failures list, and a recently-updated projects list, all read-only and linking out to their own collection view — see the "Dashboard" section in [`README.md`](README.md). Daily digest notifications are also implemented: a per-project, owner-only `notification_settings` row (webhook URL, on/off, UTC send hour) and a background worker that sends overdue/due-soon/failed-sync digests by outgoing webhook, deduped via `notification_digests` so a project never gets sent twice in one day and an empty day is never sent at all — see the "Notification digest" section in [`README.md`](README.md). Read-only merge-request/pipeline sync is also implemented (issue #111, building on [ADR-0011](docs/decisions/0011-why-merge-request-sync.md)): a `repositories` row and initial import are created automatically alongside a linked GitLab project's issue sync, `internal/mrsync` walks a repository's merge requests (webhook-primary via `Merge Request Hook`/`Pipeline Hook`, periodic catch-up via the `mr.import` outbox job) to upsert `merge_requests` rows idempotently, and a merge request whose description or branch name references an issue is linked to that issue's task through the existing `task_gitlab_links` table — see the "Merge request sync" section in [`README.md`](README.md). FlowLens never writes a merge request back to GitLab. The `MergeRequest` collection/single views are also implemented (issue #112): `GET /api/v1/projects/{projectID}/merge-requests` (filterable by `?state=`/`?author=`/`?taskId=`/`?since=`/`?until=`, sortable via `?sort=updated`) and `GET /api/v1/merge-requests/{mergeRequestID}`, plus the web screens at `/projects/[projectId]/merge-requests` and `/projects/[projectId]/merge-requests/[mrId]`, with the Task single view showing a reverse-linking "Merge requests" card — see the "Merge request views" section in [`README.md`](README.md). The merge-request/CI delivery-flow **dashboard** (aggregated metrics) is also implemented (issue #113): `GET /api/v1/projects/{projectID}/metrics?from=&to=` computes open→first-review and first-review→merge lead time (median and p90, never a mean), merge-request size distribution, pipeline success rate and merge throughput over `merge_requests` already synced, and the Project single view shows it as a stat row plus a stage-lead-time bar chart, with Median/p90 switched via a shared tab rather than drawn as two rows at once, and an `All`/`Week`/`Month`/`Year` `?interval=` selector (issue #189, building on issue #188's period bucketing) that turns each chart into one row per period plus small throughput/pipeline-success-rate trend charts — see the "Delivery metrics" and "Flow metrics" sections in [`README.md`](README.md). Merge-request size is always 0 today, since `internal/mrsync` doesn't fetch GitLab's diff stats yet. Velocity (throughput — completed tasks per period, split by `task_progress_events.actor_kind` into user/agent/unknown) is also implemented (issue #195): `GET /api/v1/projects/{projectID}/velocity?from=&to=&interval=` buckets tasks by each task's resolved completion time (`min(closed_at, first progress='done' transition)`, whichever is non-nil), not `created_at` like delivery/flow metrics, and defaults `interval` to `week` rather than "don't bucket" — see the "Velocity" section in [`README.md`](README.md), and the Project single view's "Velocity" card (issue #196), which shares the Delivery metrics card's `?from=&to=&interval=` filter. A task also carries a `size` (`xs`/`s`/`m`/`l`/`xl`, defaulting to `m`), app-only and never synced to GitLab like `priority`, which weights velocity into a parallel points series (`completedPoints`, `openTaskPoints`, `averageVelocityPoints`, `forecastPeriodsByPoints`) so throughput measures work finished rather than merely items finished — a raw count is inflated for free by splitting tasks smaller. The `xs`=1…`xl`=8 weight table lives only in `internal/velocity`; `velocity.sql` groups by size rather than summing a CASE so the weights can't drift. **Backlogs deliberately have no size** (unlike priority/progress): a backlog's size is just the sum of its tasks'. This is still **not** a story-point/estimate concept — no sprint, no per-task number typed in — and every task predating the column reads as `m`, so `sizedTaskRatio` reports how much of the completed work is actually sized. See the "Task size" section in [`README.md`](README.md). Progress sync on issue close is also implemented (issue #202): a per-project, owner-only `progress_sync_settings` row (`enabled`, off by default) via `PUT`/`GET /api/v1/projects/{projectID}/progress-sync-settings`, which — only when on, and only on a genuine `open`→`closed` transition of a task's `status` (never a redelivered or re-applied "already closed" update, never on reopen) — moves that task's `progress` to `done` and records a `task_progress_events` row with `actor_kind` `"gitlab"` (`internal/task`'s `ActorKindGitlab`, alongside `"user"`/`"agent"`). This is the one deliberate, opt-in exception to "`progress` and `status` never write each other" above; `internal/progresssync` implements it, called atomically alongside the status write from both inbound paths — `internal/webhookapply` (live webhook) and `internal/projectsync` (periodic resync) — see the "Task & backlog progress" section in [`README.md`](README.md). Login is intentionally independent of any GitLab connection. The GitLab connection used for Issue sync (and now MR sync) is scoped **per app project**, not per user — see [ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md).

Monorepo: `apps/api` (Go REST API) + `apps/web` (Next.js) + PostgreSQL.

## Commands

Run from the repo root (Makefile targets load `.env` automatically):

| Command | Purpose |
| --- | --- |
| `make dev` | Start full stack (Postgres + API + Web, hot reload) via Docker Compose |
| `make migrate` | Apply migrations by hand (`make migrate-down` rolls back one; `make migrate-create name=x` scaffolds). The API also applies its embedded migrations on startup unless `RUN_MIGRATIONS=false` |
| `make generate` | Regenerate sqlc query code — **run after editing any `.up.sql` schema or `internal/database/queries/*.sql`** — and rebundle the OpenAPI document (`apps/api/openapi/openapi.bundled.yaml`) |
| `make test` | Go + web unit tests |
| `make test-integration` | Go integration tests, gated by the `integration` build tag; needs a running Postgres with migrations applied |
| `make test-e2e` | Playwright browser e2e tests (`apps/web/e2e`); needs a running Postgres with migrations applied, starts its own API+web |
| `make lint` | golangci-lint + ESLint |
| `make build` | Build API binary and web app |
| `make build-images` | Build the release images locally, tagged `:dev` so `compose.yaml` can run them |

Running a single test:
- Go: `cd apps/api && go test ./internal/auth/ -run TestName`
- Web: `cd apps/web && npx vitest run path/to/file.test.ts -t "test name"`

## Working from a GitHub Issue

**When the work originates from a GitHub Issue, use the `/implement-issue` slash command** (`.claude/commands/implement-issue.md`) rather than reading the issue ad hoc:

```
/implement-issue 7
/implement-issue 7 フェーズ1のマイグレーションだけでいい
```

It pulls the issue body and comments into context up front, then follows a fixed order: understand → read the relevant design doc → propose a plan (mandatory pause for changes spanning more than 3 files or adding a migration) → implement → run `make test` / `make lint` and report the real output → branch as `claude/issue-<n>-<slug>` and open a PR **only after confirmation**. Keep the command file in sync when these conventions change.

The repo also has a GitHub Actions path (`.github/workflows/claude.yml`) triggered by `@claude` in an issue or comment; that one runs on CI, so it has no local Postgres. Prefer `/implement-issue` for anything touching migrations or integration tests.

**When designing or changing any web UI, follow [`docs/ui-design.md`](docs/ui-design.md)** — screens are designed object-first (OOUI), not task-first.

**When changing anything a self-hoster touches — `compose.yaml`, `.env.example`, a new environment variable, the release workflow, or a migration — update [`docs/self-hosting.md`](docs/self-hosting.md) in the same change.** It is the install/upgrade contract, not background reading, and a variable that exists but is undocumented there is a support burden. Schema migrations must stay backward compatible (new columns nullable or defaulted; a removal split across two releases), because the documented upgrade is `docker compose pull && up -d` against a live database.

**When writing or changing tests, follow [`docs/testing.md`](docs/testing.md)** — the layered strategy (integration / domain / HTTP), Fakes over Mocks, table-driven cases, and the other rules that keep the suite small and maintainable.

## Architecture

**Request flow (authenticated):** Browser → Next.js Server Component calls `getCurrentUser()` → fetches `GET /api/v1/me` from the Go API **server-to-server**, forwarding the browser's session cookie → API `requireAuth` middleware hashes the cookie token, looks up the session joined to the user, puts user in request context → handler returns JSON.

**Login flow:** the browser POSTs JSON credentials directly to the Go API (`/auth/signup`, `/auth/login`) with `credentials: "include"`; the API verifies/hashes the password with bcrypt and issues a session cookie. There is no OAuth redirect.

### API (`apps/api`) — layered, intentionally lightweight (no full Clean Architecture)
- `internal/http` — chi router, handlers, middleware (CORS, logging, auth), cookies, response helpers
- `internal/auth` — password hashing (bcrypt) and session service
- `internal/user` — user domain model + signup/authenticate service
- `internal/apitoken` — project-scoped API token domain model and service (issue, list, revoke, authenticate a bearer token); resolves a token to its project's owner, per [ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md)
- `internal/gitlab` — `gitlab.Client` interface, HTTP impl, and `FakeClient` for tests. **All GitLab CE calls go through this interface**; not yet wired into any handler — reserved for the future per-user MR/pipeline sync feature
- `internal/database` — pgx pool + sqlc-generated code in `internal/database/db` (do not hand-edit generated files)
- `internal/config` — env loading/validation
- `migrations/` — the golang-migrate SQL files, also embedded into the binary (`embed.go`) and applied at startup by `internal/database.Migrate`
- `cmd/api` — entry point, wiring, graceful shutdown
- `openapi/` — the OpenAPI 3.1 source (`openapi.yaml` + `paths/`/`components/`) and its bundled, embedded output (`openapi.bundled.yaml`, regenerated by `make generate`), served unauthenticated at `GET /openapi.yaml`/`/openapi.json`. Adding, removing or re-scoping a route in `internal/http/server.go`'s `Router()` requires updating `openapi/` in the same change — a test (`internal/http/openapi_drift_test.go`) walks the router and fails if the two disagree on the route set

### Web (`apps/web`) — Next.js App Router
- React Server Components by default; Client Components only where interactive (e.g. logout button)
- Screens are structured per object (collection view / single view) — see the UI conventions below
- `lib/api.ts` — thin server-side API client that forwards the session cookie
- `lib/config.ts` — client-safe config kept separate so client components never import server-only modules. `API_PUBLIC_URL` defaults to `""`: browser calls are **same-origin**, and `next.config.ts` rewrites proxy `/api`, `/auth` and `/webhooks` to the Go API. That is what lets one prebuilt web image serve any hostname — `NEXT_PUBLIC_*` is inlined at build time and cannot be changed on a running container. Do not reintroduce an absolute default. Note rewrites are *also* resolved at build time (serialised into `routes-manifest.json`), so the destination `API_INTERNAL_URL` (default `http://api:8080`, the compose service name) is baked into the image as well — acceptable only because it is an internal address identical for every self-hoster. `lib/api.ts`'s own use of `API_INTERNAL_URL` for Server Components *is* read at request time

### Database
- Schema owned by `golang-migrate` in `apps/api/migrations`. sqlc reads **only `*.up.sql`** (see `sqlc.yaml`) as the schema source, plus hand-written queries in `internal/database/queries`
- All tables exist from the initial migration; most are unpopulated until later phases
- UUID PKs, `timestamptz` everywhere. Natural GitLab IDs (`gitlab_group_id`, `gitlab_project_id`, `gitlab_merge_request_id`) have `UNIQUE` constraints so upserts / duplicate webhook deliveries are idempotent. `users.username`/`users.email` are also `UNIQUE`. FKs cascade on delete

## UI conventions (web)

- **Object-oriented UI (OOUI).** Design order is: extract the domain object → decide collection view vs single view → then presentation. Never start from layout.
- **Screens are named with nouns** and routes mirror the pair: `/merge-requests` (collection) → `/merge-requests/[id]` (single). No standalone task screens.
- **Actions belong to the object they act on** ("Sync now" lives in the `Project` view), and one object keeps one name across UI, route, API and schema. Note the schema still carries GitHub-era names (`repositories`, `pull_requests`) while the UI vocabulary is GitLab — see the mapping table in the guide.
- **Auth flows (login/signup) are the deliberate exception** and stay task-oriented.
- Full rules, the object model, and a per-screen checklist: [`docs/ui-design.md`](docs/ui-design.md); rationale in [ADR-0006](docs/decisions/0006-why-ooui.md).

## Key conventions & security model

- **Sessions are opaque and server-side.** The cookie holds a random token; only its SHA-256 hash is stored. HttpOnly, `Secure` in production. Revocable on logout, expire server-side.
- **Passwords are hashed with bcrypt** (`internal/auth/password.go`), never stored or logged in plaintext. Minimum length 8, enforced at signup.
- API mutations are protected by `SameSite=Lax` + locked CORS origin, plus a double-submit CSRF token: a non-HttpOnly `flowlens_csrf` cookie is set alongside the session cookie at login, and every session-authenticated POST/PATCH/PUT/DELETE must echo its value in an `X-CSRF-Token` header (`internal/http/middleware.go`'s `requireCSRF`) or is rejected with 403. It is a no-op for bearer-token requests, which carry no cookie at all.
- Secrets come only from env / `.env` (git-ignored). Never commit real credentials.
- **Outbound TLS to GitLab skips certificate verification by default** (`GITLAB_TLS_INSECURE_SKIP_VERIFY=true`), because FlowLens targets on-prem GitLab CE behind a private CA; `GITLAB_CA_CERT_FILE` takes precedence and turns verification back on. The policy is a value (`gitlab.TLSPolicy`) built once in `cmd/api/main.go` and injected into both `NewServer` and the sync workers — it is **not** a global, so a future GitHub client gets its own (verified) policy. See the "TLS for a self-hosted instance" section in [`README.md`](README.md).
- The planned "connect GitLab" feature stores the access token **per app project, not per user** ([ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md)), encrypted at rest with AES-256-GCM. No such storage exists yet.

## Self-hosting

FlowLens is distributed as prebuilt multi-arch images on
`ghcr.io/motoki1996/flowlens-{api,web}`, published by
`.github/workflows/release.yml` on a `v*` tag. A self-hoster downloads
`compose.yaml` plus `.env.example` and runs `docker compose up -d`; no
clone, no toolchain, no migrate step. `compose.yaml` publishes only the web
service, so `/healthz`, `/version` and `/metrics` are not on the public
origin. Full procedure: [`docs/self-hosting.md`](docs/self-hosting.md).

Note `compose.yaml` outranks `docker-compose.yml` in Compose's own lookup
order, which is why the `dev`/`down` Makefile targets name the dev file
explicitly.

Self-hosting-specific settings: `RUN_MIGRATIONS`, `ALLOW_SIGNUP` (the first
account is always allowed so a fresh instance can be bootstrapped),
`METRICS_TOKEN`, `TRUSTED_PROXY_HOPS` (what the per-IP rate limiters key on
behind the proxy — see `internal/http/ratelimit.go`), `FLOWLENS_REGISTRY`
(mirror for closed networks). The API binary also has two subcommands used
before any config exists: `gen-key` and `version`.

## Local ports (important)

Container Postgres is published on host port **55432** to avoid clashing with a local Postgres on 5432. Inside Docker the API reaches it as `db:5432`. Web on 4000, API on 8080.

Dev Container note: `.env` records the host port, but the Makefile keeps the environment's `DATABASE_URL` (`db:5432`, exported by compose) in preference to it, so `make migrate` works unchanged inside the container. `make dev` needs Docker, which isn't available inside the devcontainer itself — use `make dev-container` there instead, which runs the API (`air`) and Web (`npm run dev`) natively against the sibling `db` service.

## Further docs

`docs/architecture.md` for detail; `docs/ui-design.md` for the OOUI rules every web screen follows; `docs/testing.md` for the testing strategy and rules; `docs/storybook.md` for the web Storybook conventions (one story per screen, one per permission/data branch; tooling install still pending); `docs/decisions/` for ADRs (why Go+Next.js, REST, PostgreSQL, monorepo, manual-sync-first, OOUI, outbox worker, per-project GitLab connection).

`docs/self-hosting.md` is the install/upgrade contract for self-hosters;
`CONTRIBUTING.md` and `SECURITY.md` are the OSS entry points.

`docs/plans/` holds **time-limited** implementation plans, not conventions — read [`docs/plans/README.md`](docs/plans/README.md) before adding one, and delete a plan once its work ships. There is no plan in flight right now; the issue-sync MVP plan shipped and was deleted once `README.md`/`docs/ui-design.md` absorbed what survived it.
