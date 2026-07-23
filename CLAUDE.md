# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

FlowLens visualizes a team's software delivery process by collecting GitHub PR/review/CI data and surfacing delivery bottlenecks. It does **not** generate or review code with AI — the focus is measuring the review → test → merge → release flow.

**Status:** Foundation phase. Only GitHub OAuth login, user persistence, and `GET /api/v1/me` are implemented. Repository selection, PR sync, and dashboard data are not built yet, but the tables and seams for them exist.

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

## Architecture

**Request flow (authenticated):** Browser → Next.js Server Component calls `getCurrentUser()` → fetches `GET /api/v1/me` from the Go API **server-to-server**, forwarding the browser's session cookie → API `requireAuth` middleware hashes the cookie token, looks up the session joined to the user, puts user in request context → handler returns JSON.

### API (`apps/api`) — layered, intentionally lightweight (no full Clean Architecture)
- `internal/http` — chi router, handlers, middleware (CORS, logging, auth), cookies, response helpers
- `internal/auth` — `TokenCipher` (interface + AES-GCM impl), session service, OAuth state/config helpers
- `internal/user` — user domain model + upsert service
- `internal/github` — `github.Client` interface, HTTP impl, and `FakeClient` for tests. **All GitHub calls go through this interface**; pagination/rate-limits/retries will live behind it
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
- UUID PKs, `timestamptz` everywhere. Natural GitHub IDs (`github_user_id`, `github_repository_id`, `github_pull_request_id`) have `UNIQUE` constraints so upserts / duplicate webhook deliveries are idempotent. FKs cascade on delete

## Key conventions & security model

- **Sessions are opaque and server-side.** The cookie holds a random token; only its SHA-256 hash is stored. HttpOnly, `Secure` in production. Revocable on logout, expire server-side.
- **GitHub access tokens are never stored in plaintext** — encrypted via `TokenCipher` before persistence. The AES-GCM impl is local; an Azure Key Vault impl is planned behind the same interface.
- OAuth CSRF defense uses a short-lived `state` cookie compared in constant time. API mutations currently rely on `SameSite=Lax` + locked CORS origin (double-submit token planned).
- Secrets come only from env / `.env` (git-ignored). Never commit real credentials.

## Local ports (important)

Container Postgres is published on host port **55432** to avoid clashing with a local Postgres on 5432. Inside Docker the API reaches it as `db:5432`. Web on 3000, API on 8080.

Dev Container note: Makefile DB targets read `.env` (host port), so when running inside the container pass the container URL explicitly, e.g. `make migrate DATABASE_URL="$DATABASE_URL"`.

## Further docs

`docs/architecture.md` for detail; `docs/decisions/` for ADRs (why Go+Next.js, REST, PostgreSQL, monorepo, manual-sync-first).
