# FlowLens

Visualize your team's software delivery process. FlowLens connects to
GitHub and surfaces where pull requests get stuck — review latency, CI
results, and lead time to merge — so engineering leads and teams can find
and fix delivery bottlenecks with data instead of guesswork.

> **Status:** Foundation phase. Authentication (GitHub OAuth) and the app
> skeleton are implemented. Organization / repository selection and pull
> request synchronization land in later phases (see [Roadmap](#roadmap)).

## Problem

Development teams feel that pull requests sit too long, reviews stall, and
releases slip — but they lack the data to say *where* and *why*. GitHub
holds the raw signal, but it is spread across PRs, reviews, and CI runs and
is hard to see as a process.

## Solution

FlowLens collects development data from GitHub and turns it into
process-oriented views: open/draft/review-waiting counts, stale PRs, recent
and merged PRs, and average time-to-merge. It does **not** generate or
review code with AI; its focus is measuring the review → test → merge →
release flow.

## Architecture

FlowLens is a monorepo with two applications and a database.

```text
Browser ──▶ Next.js (web)  ──server-to-server──▶  Go API  ──▶  PostgreSQL
   │            App Router                         REST            │
   │                                                 │             │
   └──────── GitHub OAuth redirect ──▶ Go API ──▶ GitHub API ──────┘
```

- **Web** (`apps/web`): Next.js App Router, React Server Components by
  default, Tailwind CSS. Server Components call the API server-to-server and
  forward the session cookie.
- **API** (`apps/api`): Go REST API. Layers are separated into HTTP
  handlers, services/use cases, a repository layer (sqlc-generated
  queries), an external GitHub client, and domain models.
- **Database**: PostgreSQL, schema managed by `golang-migrate`.

See [docs/architecture.md](docs/architecture.md) for details and
[docs/decisions](docs/decisions) for the architecture decision records.

## Technology stack

| Area            | Choice                                             |
| --------------- | -------------------------------------------------- |
| Web             | Next.js 15 (App Router), React 19, TypeScript, Tailwind CSS 4 |
| API             | Go, chi router, `net/http`                         |
| DB access       | PostgreSQL, pgx, sqlc (type-safe queries)          |
| Migrations      | golang-migrate                                     |
| Auth            | GitHub OAuth, server-side sessions, HttpOnly cookie |
| Secrets/crypto  | AES-256-GCM token cipher behind an interface (Key Vault later) |
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
3. Fill in `TOKEN_ENCRYPTION_KEY` and the GitHub OAuth values in `.env`
   (see steps below), then start the app from inside the container:

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

Generate a token encryption key and paste it into `.env` as
`TOKEN_ENCRYPTION_KEY`:

```bash
openssl rand -base64 32
```

### 2. Create a GitHub OAuth App

Go to <https://github.com/settings/developers> → **New OAuth App**:

- **Homepage URL:** `http://localhost:3000`
- **Authorization callback URL:** `http://localhost:8080/auth/github/callback`

Copy the Client ID and generate a Client Secret into `.env`
(`GITHUB_OAUTH_CLIENT_ID`, `GITHUB_OAUTH_CLIENT_SECRET`).

### 3. Start the stack

```bash
make dev          # docker compose up --build
```

In another terminal, apply migrations:

```bash
make migrate
```

Then open <http://localhost:3000> and sign in with GitHub.

## Environment variables

All variables are documented in [`.env.example`](.env.example). Key ones:

| Variable                     | Purpose                                          |
| ---------------------------- | ------------------------------------------------ |
| `DATABASE_URL`               | Postgres connection string (host uses port 55432)|
| `GITHUB_OAUTH_CLIENT_ID`     | GitHub OAuth App client ID                       |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth App client secret                   |
| `TOKEN_ENCRYPTION_KEY`       | base64 32-byte AES key for the local token cipher|
| `SESSION_TTL_HOURS`          | Session lifetime                                 |
| `ENCRYPTION_KEY`             | base64 32-byte AES-256 key encrypting secrets at rest (GitLab tokens, webhook secrets) |
| `APP_PUBLIC_URL`             | Public URL for GitLab webhook delivery; unset skips auto-registration |
| `SYNC_WORKER_ENABLED`        | Whether the in-process sync worker runs (default `true`) |
| `SYNC_WORKER_POLL_INTERVAL`  | Sync worker poll interval (default `5s`)         |
| `WEB_BASE_URL` / `API_BASE_URL` | Public URLs for redirects and CORS            |
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

- **Go**: use-case unit tests, a fake GitHub client, HTTP handler tests, and
  an integration test that runs the generated queries against a live
  Postgres (`make test-integration`, gated by the `integration` build tag).
- **Web**: API client tests, a component test, and a dashboard render test
  (Vitest).

```bash
make test
```

## Current limitations

- Only GitHub OAuth login, user persistence, and `GET /api/v1/me` are
  implemented. There is no repository selection, PR sync, or dashboard data
  yet.
- The token cipher is the local AES-GCM implementation; the Azure Key Vault
  implementation is not written yet (the interface is in place).
- CSRF protection for the API relies on `SameSite=Lax` cookies plus a
  locked-down CORS origin; a double-submit token is planned.
- Integration tests assume migrations are already applied.

## Roadmap

1. **Foundation (done):** monorepo, Docker Compose, migrations, health
   check, GitHub OAuth, sessions, `GET /api/v1/me`.
2. **Organizations & repositories:** list from GitHub, import, select active
   repositories.
3. **Pull request sync:** manual sync button, idempotent upserts, rate-limit
   and pagination handling.
4. **Dashboard & PR views:** metrics, list with filters, detail page,
   empty/loading/error states.
5. **Automation:** webhooks (with duplicate-delivery handling) and scheduled
   sync via Azure Service Bus.
6. **Azure deployment:** Container Apps, Azure Database for PostgreSQL, Key
   Vault, Application Insights.
