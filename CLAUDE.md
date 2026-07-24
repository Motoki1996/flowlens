# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

FlowLens visualizes a team's software delivery process by collecting GitLab CE MR/review/CI data and surfacing delivery bottlenecks. It does **not** generate or review code with AI — the focus is measuring the review → test → merge → release flow.

**Status:** Foundation phase. Only local username/password login (with signup), user persistence, and `GET /api/v1/me` are implemented. Project selection, MR sync, and dashboard data are not built yet, but the tables and seams for them exist. Login is intentionally independent of any GitLab connection — a per-user GitLab CE personal access token (for MR/pipeline sync) is a separate, not-yet-built feature.

Monorepo: `apps/api` (Go REST API) + `apps/web` (Next.js) + PostgreSQL.

## Commands

Run from the repo root (Makefile targets load `.env` automatically):

| Command | Purpose |
| --- | --- |
| `make dev` | Start full stack (Postgres + API + Web, hot reload) via Docker Compose |
| `make migrate` | Apply migrations (`make migrate-down` rolls back one; `make migrate-create name=x` scaffolds) |
| `make generate` | Regenerate sqlc query code — **run after editing any `.up.sql` schema or `internal/database/queries/*.sql`** |
| `make test` | Go + web unit tests |
| `make test-integration` | Go integration tests, gated by the `integration` build tag; needs a running Postgres with migrations applied |
| `make lint` | golangci-lint + ESLint |
| `make build` | Build API binary and web app |

Running a single test:
- Go: `cd apps/api && go test ./internal/auth/ -run TestName`
- Web: `cd apps/web && npx vitest run path/to/file.test.ts -t "test name"`

**When writing or changing tests, follow [`docs/testing.md`](docs/testing.md)** — the layered strategy (integration / domain / HTTP), Fakes over Mocks, table-driven cases, and the other rules that keep the suite small and maintainable.

## Architecture

**Request flow (authenticated):** Browser → Next.js Server Component calls `getCurrentUser()` → fetches `GET /api/v1/me` from the Go API **server-to-server**, forwarding the browser's session cookie → API `requireAuth` middleware hashes the cookie token, looks up the session joined to the user, puts user in request context → handler returns JSON.

**Login flow:** the browser POSTs JSON credentials directly to the Go API (`/auth/signup`, `/auth/login`) with `credentials: "include"`; the API verifies/hashes the password with bcrypt and issues a session cookie. There is no OAuth redirect.

### API (`apps/api`) — layered, intentionally lightweight (no full Clean Architecture)
- `internal/http` — chi router, handlers, middleware (CORS, logging, auth), cookies, response helpers
- `internal/auth` — password hashing (bcrypt) and session service
- `internal/user` — user domain model + signup/authenticate service
- `internal/gitlab` — `gitlab.Client` interface, HTTP impl, and `FakeClient` for tests. **All GitLab CE calls go through this interface**; not yet wired into any handler — reserved for the future per-user MR/pipeline sync feature
- `internal/database` — pgx pool + sqlc-generated code in `internal/database/db` (do not hand-edit generated files)
- `internal/config` — env loading/validation
- `cmd/api` — entry point, wiring, graceful shutdown

### Web (`apps/web`) — Next.js App Router
- React Server Components by default; Client Components only where interactive (e.g. logout button)
- `lib/api.ts` — thin server-side API client that forwards the session cookie
- `lib/config.ts` — client-safe config kept separate so client components never import server-only modules

### Database
- Schema owned by `golang-migrate` in `apps/api/migrations`. sqlc reads **only `*.up.sql`** (see `sqlc.yaml`) as the schema source, plus hand-written queries in `internal/database/queries`
- All tables exist from the initial migration; most are unpopulated until later phases
- UUID PKs, `timestamptz` everywhere. Natural GitLab IDs (`gitlab_group_id`, `gitlab_project_id`, `gitlab_merge_request_id`) have `UNIQUE` constraints so upserts / duplicate webhook deliveries are idempotent. `users.username`/`users.email` are also `UNIQUE`. FKs cascade on delete

## Key conventions & security model

- **Sessions are opaque and server-side.** The cookie holds a random token; only its SHA-256 hash is stored. HttpOnly, `Secure` in production. Revocable on logout, expire server-side.
- **Passwords are hashed with bcrypt** (`internal/auth/password.go`), never stored or logged in plaintext. Minimum length 8, enforced at signup.
- API mutations currently rely on `SameSite=Lax` + locked CORS origin (double-submit token planned).
- Secrets come only from env / `.env` (git-ignored). Never commit real credentials.
- A future "connect GitLab" feature will need to store a per-user GitLab personal access token encrypted at rest — no such storage exists yet.

## Local ports (important)

Container Postgres is published on host port **55432** to avoid clashing with a local Postgres on 5432. Inside Docker the API reaches it as `db:5432`. Web on 3000, API on 8080.

Dev Container note: Makefile DB targets read `.env` (host port), so when running inside the container pass the container URL explicitly, e.g. `make migrate DATABASE_URL="$DATABASE_URL"`. `make dev` needs Docker, which isn't available inside the devcontainer itself — use `make dev-container` there instead, which runs the API (`air`) and Web (`npm run dev`) natively against the sibling `db` service.

## Further docs

`docs/architecture.md` for detail; `docs/testing.md` for the testing strategy and rules; `docs/storybook.md` for the web Storybook conventions (one story per screen, one per permission/data branch; tooling install still pending); `docs/decisions/` for ADRs (why Go+Next.js, REST, PostgreSQL, monorepo, manual-sync-first).
