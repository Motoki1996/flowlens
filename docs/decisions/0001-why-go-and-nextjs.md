# 0001. Why Go and Next.js

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

FlowLens needs a backend that talks to the GitHub API, performs periodic and
on-demand synchronization, and serves a REST API, plus a frontend that
renders data-heavy dashboards. The target deployment is Azure Container
Apps. We want a small, understandable stack that a team can operate.

## Decision

Use **Go** for the API and **Next.js (App Router, TypeScript)** for the web
app.

- Go gives a simple concurrency model (useful for paginated, rate-limited
  GitHub syncing), fast startup, small static binaries that suit Container
  Apps, and strong standard-library HTTP support.
- Next.js with React Server Components lets us keep most data fetching on the
  server, ship less client JavaScript, and colocate routing with the UI.
  TypeScript keeps the API contract typed end to end.

## Consequences

- The repo mixes two toolchains (Go modules and npm); the monorepo tooling
  (`make`, Docker Compose) absorbs this.
- Server Components require care about the server/client boundary (e.g.
  keeping `next/headers` out of client components).
- We get idiomatic, well-supported ecosystems on both sides and a natural
  fit for the Azure deployment plan.
