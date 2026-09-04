# Development setup

Working on FlowLens itself. If you only want to *run* FlowLens, read
[docs/self-hosting.md](self-hosting.md) instead.

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
issues, see [GitLab CE connection & sync](features/gitlab-sync.md).

## Environment variables

All variables are documented in [`.env.example`](../.env.example), and the ones
that matter for a self-hosted install are tabulated in
[docs/self-hosting.md](self-hosting.md#configuration-reference). Key
ones for development:

| Variable                     | Purpose                                          |
| ---------------------------- | ------------------------------------------------ |
| `DATABASE_URL`               | Postgres connection string (host uses port 55432)|
| `SESSION_TTL_HOURS`          | Session lifetime                                 |
| `ENCRYPTION_KEY`             | **Required.** base64 32-byte AES-256 key encrypting secrets at rest (GitLab access tokens, webhook secrets) — see [Generating `ENCRYPTION_KEY`](features/gitlab-sync.md#generating-encryption_key) |
| `APP_PUBLIC_URL`             | Public URL GitLab must be able to reach to deliver webhooks. Optional; unset skips webhook auto-registration and falls back to manual sync — see [Operating without `APP_PUBLIC_URL`](features/gitlab-sync.md#operating-without-app_public_url) |
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
has its own guide: [**docs/self-hosting.md**](self-hosting.md). The
short version is at the top of this file.

What the deployment is made of:

- **Prebuilt multi-arch images** (amd64 + arm64) on
  `ghcr.io/motoki1996/flowlens-{api,web}`, published by
  [`.github/workflows/release.yml`](../.github/workflows/release.yml) on a
  `v*` tag. Both are minimal, non-root runtime stages —
  `apps/api/Dockerfile`'s `runtime` target, and `apps/web/Dockerfile`'s
  `runner` target built on `output: "standalone"` so only the files the
  Next.js server actually needs are in the image.
- **[`compose.yaml`](../compose.yaml)**, the file a self-hoster downloads. It
  pulls those images and needs nothing else from this repository.
  [`compose.tls.yaml`](../compose.tls.yaml) overlays HTTPS via Caddy.
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

