# 0004. Why a monorepo

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

FlowLens has a Go API and a Next.js web app that evolve together and share a
contract (OpenAPI) and documentation. We had to choose between separate
repositories and a single monorepo.

## Decision

Keep both applications in **one monorepo** under `apps/api` and `apps/web`,
with shared `docs/`, a root `Makefile`, `docker-compose.yml`, and a single
`.env.example`.

## Consequences

- One clone and one `make dev` bring the whole stack up; contract changes to
  the API and the web client land in a single, atomic commit.
- Shared documentation and ADRs live next to the code they describe.
- The trade-off is a repo that mixes Go and Node toolchains and CI that must
  understand both; the `Makefile` and per-app Dockerfiles contain this.
- If the apps ever need independent release cadences or ownership, they can
  be split later; the directory boundaries already isolate them.
