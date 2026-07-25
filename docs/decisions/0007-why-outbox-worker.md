# 0007. Why a Postgres outbox and in-process worker for GitLab sync

- **Status:** Accepted
- **Date:** 2026-07-25

## Context

Issue sync is bidirectional: a task edited in FlowLens must reach the GitLab CE
issue, and a GitLab webhook must reach the task. Pushing to GitLab inline from
the HTTP handler is the obvious shortcut and the wrong one — a GitLab outage,
a rate limit, or a slow response would fail a request whose local write already
succeeded, leaving the two sides silently divergent with no record of the
attempt.

The usual answer is a queue (Redis, a message bus). [ADR-0003](0003-why-postgresql.md)
already commits to PostgreSQL as the only datastore, and adding a second piece
of infrastructure for a self-hosted single-team tool costs more in operation
than it returns.

## Decision

Use the **transactional outbox** pattern with a **worker running in the API
process**:

- A mutation writes the task and inserts a `sync_jobs` row **in the same
  transaction**. Either both land or neither does, so an accepted request is
  always eventually pushed.
- The worker polls with `SELECT … FOR UPDATE SKIP LOCKED`, retries with capped
  exponential backoff, and on final failure records the error on
  `task_gitlab_links.sync_status = 'failed'` so the UI can show it and offer a
  retry.
- `dedupe_key` (e.g. `issue.update:<task_id>`) collapses rapid repeated edits
  into one pending job.
- Inbound webhooks are the mirror image: the endpoint only records the delivery
  (`webhook_events`, `status='pending'`) and returns 200 fast; the same worker
  applies it.

No Redis, no message bus. `SYNC_WORKER_ENABLED` and `SYNC_WORKER_POLL_INTERVAL`
control the worker.

## Consequences

- Sync state is queryable with plain SQL — pending, failed, and retried jobs are
  visible in the same database as the data they act on, and a failed push is
  never invisible.
- `SKIP LOCKED` makes the claim safe across multiple API replicas without
  leader election, so scaling out does not require rework.
- Polling adds latency between the write and the GitLab push, bounded by the
  poll interval. This is acceptable: the sibling guarantee we care about is
  *eventual and observable*, not instant.
- Deploying the API restarts the worker. Jobs are durable in Postgres, so an
  in-flight job is retried rather than lost, but at-least-once delivery means
  every job kind must be **idempotent** — the same constraint
  [ADR-0005](0005-why-manual-sync-first.md) already imposed on sync.
- The worker competes with request handling for process resources. If that
  becomes a problem, the outbox table is unchanged by moving the worker to its
  own binary — that is the intended escape hatch, and the reason the worker is
  behind a config flag from day one.
- Loop prevention becomes a property of the code path rather than of the queue:
  applying an inbound event must never enqueue an outbound job. This is enforced
  by a repository method with no outbound side effects, plus a content
  fingerprint check, and is covered by dedicated tests.
