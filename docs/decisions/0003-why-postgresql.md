# 0003. Why PostgreSQL

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

FlowLens stores relational data — users, organizations, repositories, pull
requests, reviewers, and sync runs — with clear foreign-key relationships,
and it will compute aggregate metrics (counts, averages, time windows).
Azure offers a managed PostgreSQL service.

## Decision

Use **PostgreSQL**, accessed with **pgx** and **sqlc** for type-safe
queries, with schema managed by **golang-migrate**.

## Consequences

- Strong relational modeling and rich SQL (window functions, interval math)
  fit the metric computations planned for the dashboard.
- sqlc generates type-safe Go from plain SQL, giving compile-time safety
  without hiding queries behind an ORM — matching the "avoid over-abstraction"
  goal. The cost is a code-generation step (`make generate`).
- `Azure Database for PostgreSQL` provides a managed production path.
- Natural GitHub IDs get `UNIQUE` constraints so synchronization can upsert
  idempotently.
