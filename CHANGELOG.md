# Changelog

Notable changes per release. FlowLens follows semantic versioning, and a
release that needs a self-hoster to do anything beyond `docker compose pull
&& docker compose up -d` is marked **⚠️ Breaking** with the steps written
out — see [`docs/self-hosting.md`](docs/self-hosting.md) for the upgrade
procedure itself.

## Unreleased

### Added

- **Epics: an optional rung between a backlog and its tasks**
  ([ADR-0012](docs/decisions/0012-why-an-epic-layer.md)). An epic is the
  coarse unit — one screen, one endpoint group — that a refined backlog is
  cut into before each of those is broken down into tasks. It is shaped as
  "a backlog that lives inside a backlog": the same fields, minus `size`
  (an epic's size is the sum of its tasks'), plus `backlogId`. Using them
  is entirely optional — a backlog can still hold tasks directly, and every
  existing backlog keeps working untouched.

  Epics are **app-only end to end**: no epic is ever created in, or read
  from, GitLab, and moving a task between epics never enqueues a sync job.
  Deleting an epic drops its tasks back into their backlog rather than
  deleting them.

  New endpoints, all on the API-token allowlist so an agent can drive the
  whole breakdown: `GET`/`POST /api/v1/projects/{projectID}/epics`,
  `GET`/`PATCH`/`DELETE /api/v1/epics/{epicID}`, and
  `PATCH /api/v1/epics/{epicID}/tasks` (the epic's complete `taskIds` set,
  declarative and all-or-nothing). Tasks gain a writable `epicId` on
  create, update and bulk create, and `?epic_id=` on both task
  collections. New screens at `/projects/{projectId}/epics` and
  `/epics/{epicId}`, with Board, List and Timeline view modes like the
  Backlog collection.
- **An epic's own base branch and change scope.** An epic can carry its own
  `baseBranch`, `allowedScope`, `forbiddenScope` and
  `defaultLinkedGitlabProjectId`. `GET /api/v1/tasks/{taskID}/context`
  resolves them **epic first, then backlog, per field**, so an epic that
  overrides only the branch still inherits its backlog's scope.
- **An epic's provisional estimate.** `estimatedPoints` is an epic's
  pre-breakdown guess, on the same raw scale velocity weights a task's
  `size` onto. It is deliberately not a `size` and not one of `xs`..`xl`:
  it stands in only until the epic has tasks, after which the sum of those
  tasks' sizes is authoritative and the estimate is never consulted again —
  and never cleared, so a later estimate-vs-actual calibration still has
  both numbers. `null` means unestimated; `0` is rejected so that stays
  distinguishable.

  `GET /api/v1/projects/{projectID}/velocity` gains
  `unbrokenDownEpicPoints`, `unestimatedEpicCount` and `openPointsTotal`,
  the new numerator of `forecastPeriodsByPoints`. Epics that already have
  tasks are excluded in SQL so nothing is counted twice, and the
  count-denominated `forecastPeriods`/`openTaskCount` stay task-only. The
  Velocity card now says when the points forecast is a lower bound rather
  than presenting it as the whole picture.
- **A task carries its epic with it.** `GET /api/v1/tasks/{taskID}` now
  embeds the task's epic as an `epic` object — name, description, dates,
  priority, progress, `baseBranch`, `allowedScope`, `forbiddenScope`,
  `estimatedPoints` — so a caller holding nothing but a task id sees the
  rung above it without a second call. The values are the epic's **own**:
  nothing is resolved against the backlog below it, which remains
  `GET /api/v1/tasks/{taskID}/context`'s job and stays what an agent should
  follow. Only the single-task read populates it; every list carries
  `epicId` alone and sends `"epic": null`, as does a task with no epic.
  Additive — the key is new, nothing existing changed shape.
- **An index on `tasks(epic_id)`** (migration `000035`, partial on non-null).
  The epic collection's task counts and velocity's "has this epic been
  broken down yet" anti-join both filter on `epic_id` alone, which the
  `project_id`-leading index from `000032` could not serve. Additive, no
  action needed.
- **`@motokis-lab/agent-kit` 0.2.0** teaches the epic rung: a new
  `/flowlens:breakdown-epics` command splits a refined backlog into
  estimated epics, and `/flowlens:breakdown` now takes either a backlog or
  an epic. `init` installs four slash commands rather than three.

### Removed

- **⚠️ Breaking — manual (drag-and-drop) ordering.** Tasks, backlogs and
  epics no longer carry a `position`, and the three bulk-reorder endpoints
  (`PATCH /api/v1/projects/{projectID}/tasks/order`, `.../backlogs/order`,
  `.../epics/order`) are gone. A list's default order is now its objects'
  creation order, and `?sort=` (priority, progress, due date, size, recently
  updated) is the way to order one deliberately; the web app's List views
  have lost their drag handles and move-up/move-down buttons, and the sort
  menu's "Manual order" is now "Default order".

  **What a self-hoster must do:** nothing beyond the usual `docker compose
  pull && docker compose up -d`. Migration `000034` drops the `position`
  column from `tasks`, `backlogs` and `epics`; the manual order every row
  carried is discarded and cannot be recovered by rolling the migration
  back. Take a database dump first if that order matters to you.

  **This upgrade is one-way: you cannot downgrade to v0.1.2 afterwards.**
  Unlike every migration before it, `000034` removes a column the previous
  release's code still reads, and the API only ever applies *up* migrations
  at startup. Once it has run, pointing `FLOWLENS_VERSION` back at v0.1.2
  leaves that binary querying a `position` column that no longer exists,
  and every task and backlog list fails. If you want a way back, take the
  database dump *before* upgrading and restore it alongside the older
  image — rolling the image back on its own is not enough.

  **What an API client must do:** stop sending `position` in a create or
  update body (it is now ignored — the field no longer exists on any
  request or response schema) and stop calling the three `.../order`
  endpoints, which now return 404.

## v0.1.2 — 2026-08-23

### Added

- **A FlowLens assignee for tasks and backlogs.** Both objects now carry an
  optional `assigneeUserId`, naming the project member who owns the work —
  independent of a task's GitLab issue assignee, which keeps syncing as
  before. Setting the FlowLens assignee also assigns the GitLab issue when
  that member has a GitLab identity registered for the project; a GitLab-side
  change never writes back. `?assignee=` on the task and backlog collections
  now accepts a user UUID or `unassigned`, not just `me`, so a lead can see
  what any member is carrying. Upgrading needs no action: the migration is
  additive and backfills the new column from existing GitLab assignees where
  they unambiguously map to a project member.

### Changed

- **The Velocity chart's bars are narrower.** A range holding only a handful
  of periods drew bars wide enough to read as a block diagram; they are now
  capped at a readable width with a wider gap between periods. Presentation
  only — no metric changed.
- **A faster dev loop in the dev container** (contributors only, nothing for
  a self-hoster to do). `next dev` runs under Turbopack and `dev.sh` warms
  the common routes in the background, cutting the wait for the first web
  screen after `make dev-container`.

## v0.1.1 — 2026-08-21

### Added

- **`@motokis-lab/agent-kit` published to npm.** The release workflow now
  publishes `packages/agent-kit` alongside the Docker images, whenever its
  own `package.json` version is new. `npx @motokis-lab/agent-kit init` is
  now installable without a local clone of this repository.

## v0.1.0 — 2026-08-20

The first public release. Everything below is new, so this entry describes
what FlowLens is rather than what changed.

### Added

- **Task tracker kept 1:1 with GitLab CE issues.** Projects, backlogs and
  tasks, a per-project GitLab connection ([ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md)),
  linked GitLab projects, an outbox-backed sync worker and a webhook
  receiver. A task's title, description, assignee and open/closed status
  sync both ways; FlowLens's own fields never do.
- **FlowLens-only planning fields**, app-only and never written to GitLab:
  `priority`, `progress` (FlowLens's four-stage work state, distinct from
  the GitLab issue `status`), `size`, manual `position` with bulk
  reordering, `startDate`/`dueOn` and task dependencies. Progress can
  optionally follow an issue close, per project and off by default.
- **Board, Timeline (Gantt) and List view modes** on the Task and Backlog
  collections, plus a cross-project task collection and a dashboard of
  overdue, due-soon and failed-sync work.
- **Read-only merge-request and pipeline sync**, with merge requests linked
  back to a task through the issue they reference
  ([ADR-0011](docs/decisions/0011-why-merge-request-sync.md)). FlowLens
  never writes a merge request back to GitLab.
- **Delivery, flow and velocity metrics** computed over synced merge
  requests and completed tasks — lead time (median and p90, never a mean),
  merge-request size distribution, pipeline success rate, merge throughput
  and completed-task throughput split by whether a human or an agent
  finished the work.
- **An AI-agent-facing API.** Project-scoped API tokens over
  `Authorization: Bearer` ([ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md)),
  a task context endpoint, a task activity log, an OpenAPI 3.1 document
  served at `GET /openapi.yaml`, and `@motokis-lab/agent-kit`
  (`npx @motokis-lab/agent-kit init`) which installs a Claude Code skill and
  slash commands into the repository an agent works in.
- **Accounts and access.** Local username/password login with server-side
  opaque sessions, project membership with owner/member/viewer roles
  ([ADR-0010](docs/decisions/0010-why-project-membership.md)), and
  single-use expiring invite links.
- **Daily digest notifications** by outgoing webhook, per project.
- **Self-hosting.** `compose.yaml` pulls prebuilt images from GHCR, so an
  install is that file plus a `.env` with no clone, no toolchain and no
  separate migrate step. `compose.tls.yaml` adds HTTPS via Caddy.
- The API applies its own embedded schema migrations on startup
  (`RUN_MIGRATIONS`, default on).
- `flowlens-api gen-key` prints a ready-to-paste `ENCRYPTION_KEY`, so
  generating one needs nothing but the image itself.
- **Invite links** (`POST`/`GET /api/v1/projects/{projectID}/invites`,
  `DELETE /api/v1/invites/{inviteID}`, `POST /api/v1/invites/accept`,
  `GET /auth/invites/{token}`, and the `/invites/[token]` screen). A
  single-use, expiring link lets someone with no account create one and join
  one project at a named role, so an instance can keep `ALLOW_SIGNUP=false`
  and still onboard people — previously adding a member required the person
  to be registered already, which closed registration made impossible. Only
  the link's hash is stored, and no email is sent: FlowLens has no mail
  transport. Owner-only, session-only, never reachable by a project API
  token.
- **Changing your own password**: `PUT /api/v1/me/password` and a form on
  **Settings → Password**. A successful change revokes every session the
  account holds and issues a fresh one, so no older token survives it while
  the caller stays signed in. Session-only — a project API token can never
  call it. There is no reset-by-email flow; `flowlens-api hash-password`
  plus [Recovering a lost
  password](docs/self-hosting.md#recovering-a-lost-password) is the
  operator's path for an account that is locked out.
- `GET /version` and `flowlens-api version` report the running build.
- `ALLOW_SIGNUP` closes registration on an instance whose accounts already
  exist. The first account is always allowed, so a fresh instance can be
  bootstrapped with it already off.
- `METRICS_TOKEN` puts `GET /metrics` behind a bearer token.
- `TRUSTED_PROXY_HOPS` tells the per-IP rate limiters how many proxies to
  trust in `X-Forwarded-For`.
- Multi-arch (amd64 + arm64) release images on `ghcr.io`, plus an offline
  image bundle attached to each release for air-gapped installs.
- `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, `SECURITY.md`, issue
  templates, and [`docs/self-hosting.md`](docs/self-hosting.md).
