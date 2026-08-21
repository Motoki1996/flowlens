# Changelog

Notable changes per release. FlowLens follows semantic versioning, and a
release that needs a self-hoster to do anything beyond `docker compose pull
&& docker compose up -d` is marked **⚠️ Breaking** with the steps written
out — see [`docs/self-hosting.md`](docs/self-hosting.md) for the upgrade
procedure itself.

## Unreleased

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
