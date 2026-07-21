# 0005. Why manual sync for the first MVP

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

FlowLens ultimately needs fresh GitHub data via webhooks and scheduled jobs.
Those introduce real complexity: webhook signature verification, duplicate
delivery handling, background workers, a message bus (Azure Service Bus),
and retry/backoff around GitHub rate limits. Building all of that before any
data is visible would delay validating the core product.

## Decision

For the first MVP, synchronize pull requests with a **manual "Sync" button**
that triggers `POST /api/v1/repositories/{id}/sync`. Automatic sync
(webhooks and scheduled jobs) is deferred to a later phase.

## Consequences

- We can ship and validate the dashboard and PR views with a simple,
  synchronous, request-scoped sync path first.
- The sync logic is designed to be **idempotent** (upsert by GitHub ID) from
  day one, so moving to webhooks or scheduled runs later does not require
  reworking it — only adding triggers.
- Data is only as fresh as the last manual sync until automation lands; this
  is acceptable for early users and clearly documented.
- The `sync_runs` table and the `github.Client` seam are already in place to
  support the automated phase.
