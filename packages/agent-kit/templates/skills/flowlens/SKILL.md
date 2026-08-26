---
name: flowlens
description: Shared knowledge for working FlowLens tasks from this repository — auth, the task lifecycle and its call order, branch naming, and what each /flowlens: command does. Read before calling the FlowLens API directly.
---

# FlowLens

FlowLens is the task tracker this repository's tasks live in, kept 1:1 with
GitLab CE issues. This skill covers what the four `/flowlens:` commands
share: how to authenticate, and — the part an OpenAPI spec can't express —
**what order to call things in**.

Config for this repo's connection lives in `.flowlens/config.json`
(`baseUrl`, `projectId`), written by `agent-kit init` and committed to
git. The full API schema is `.flowlens/openapi.yaml`, fetched from the
connected instance — read it whenever you need an exact request/response
shape; this file stays short on purpose (progressive disclosure). If
`.flowlens/` doesn't exist yet, run `agent-kit init --url <url>` with
`FLOWLENS_API_TOKEN` set to populate it.

## Authentication

- Every call: `Authorization: Bearer <token>`. Read the token from an
  environment variable (e.g. `FLOWLENS_API_TOKEN`) — never hardcode it or
  write it into `.flowlens/`.
- No CSRF header is needed for bearer requests (FlowLens's CSRF check is a
  no-op for token auth; it only applies to browser session cookies).
- A token is scoped to one project (`.flowlens/config.json`'s `projectId`)
  and to a fixed route allowlist. If a call 404s where you expected data,
  check the route is actually reachable by a token before assuming the
  resource doesn't exist — some routes are session-only, permanently
  (project/GitLab-connection/API-token/membership management).

## Collection endpoints are paged

Every list — `GET /api/v1/projects/{projectId}/{tasks,backlogs,epics}` and
`GET /api/v1/tasks` — answers with an object, not a bare array. Tasks:

```json
{ "tasks": [...], "nextPage": 2, "totalCount": 137, "openCount": 96 }
```

Read the rows from `tasks`, not from the response itself. `nextPage` is `0`
on the last page; `?page=` and `?per_page=` (default 50, capped at 200) walk
them. `totalCount` is the whole match, so a page's length never tells you how
much is there. Backlogs and epics are not paged and still return arrays.

## Task lifecycle (the order matters)

```
GET  /api/v1/tasks/{taskID}/context          # acceptance criteria, scope, GitLab issue iid
POST /api/v1/tasks/{taskID}/design-started    # the moment design work starts
     → POST /api/v1/tasks/{taskID}/comments       (post the design)
     → PUT  /api/v1/tasks/{taskID}/ai-context      (record acceptance criteria)
POST /api/v1/tasks/{taskID}/implementation-started
     → PATCH /api/v1/tasks/{taskID}  {"progress": "in_progress"}
     → branch, per naming rule below
     → open an MR whose description includes "Closes #<issueIid>"
POST /api/v1/tasks/{taskID}/close             # once the work is done
```

- `design_started_at` / `implementation_started_at` feed FlowLens's design
  and implementation lead-time metrics. Skip a marker and that stage reads
  empty — it is not inferred from anything else.
- **`progress` is a separate axis from the markers above.** Nothing moves
  it automatically; PATCH it yourself or the task sits at `not_started` on
  every board and dashboard regardless of what you actually did. Set
  `in_progress` when you start, `on_hold` when blocked on something outside
  the task, `done` when finished. Never PATCH `status` — that mirrors the
  GitLab issue's open/closed state and syncs automatically.
- `GET .../context`'s `progressGuidance` field carries this same
  instruction — trust it over stale caches of this file.

## Branch naming (required for MR ↔ task linking)

A merge request is linked to its task **by branch name**, matched against:

```
(?:^|/)(?:issue-)?(\d+)(?:-|$)
```

So `123-fix-login`, `issue-123-fix-login`, and `feat/issue-123-fix-login`
all work — the GitLab issue **iid** (not the FlowLens task ID) must appear
in that position. Get the iid from `GET .../context`'s `gitlab.issueIid`.

**A branch that doesn't match leaves the review/merge stages of FlowLens's
flow metrics empty** — the design/implementation markers above only pay
off if the MR is actually linked.

Also put `Closes #<issueIid>` in the MR description: FlowLens reads it to
link the MR to the task, and GitLab uses it to close the issue on merge.

## Base branch

A backlog may name its own `baseBranch` (`GET` on the backlog, or via the
task's `context` response) — branch from that, not from the repo's default
branch, when the task belongs to one.

## Closed backlogs and epics

A backlog or an epic can be closed — it shipped, or it was dropped. Closed
ones are **left out of the list endpoints by default**, so
`GET /api/v1/projects/{projectID}/backlogs` returns only live work; pass
`?status=all` (or `?status=closed`) to see the rest, and `GET` on the object
itself always works whatever its status.

Closing does **not** cascade: a task inside a closed backlog or epic keeps
its own `status` and `progress`, and stays workable. Don't infer that a task
is finished from its backlog being closed — and don't close a backlog or epic
to signal that its tasks are done. Close the tasks.

## Concurrency: one agent at a time

Running multiple agents against `/flowlens:work` concurrently is **not
supported** — there is no claim/lock endpoint yet, so two agents can race
to pick up the same task. Work tasks sequentially, one `/flowlens:work`
invocation at a time, until a claim mechanism exists.

## Commands

Call order: `refine-backlog` → `breakdown-epics` (optional) →
`breakdown` → `work`.

- `/flowlens:refine-backlog <backlogId>` — turns a backlog's rough
  description into numbered requirements (`R1`, `R2`, …) and a
  `baseBranch`, written back to the backlog.
- `/flowlens:breakdown-epics <backlogId>` — splits a refined backlog into
  **epics**: coarse units of a few tasks each (one screen, one endpoint
  group), each able to carry its own base branch and change scope.
  Optional — a backlog small enough to break straight into tasks skips it.
  Each epic also gets an `estimatedPoints`: a **provisional** estimate that
  exists only because the epic has no tasks yet. It is what keeps
  unbroken-down work visible in the project's velocity forecast, and it
  stops being consulted the moment `/flowlens:breakdown` creates the epic's
  tasks — from then on the sum of those tasks' `size` is the truth. It is
  never deleted, so do not "correct" it after a breakdown: the original
  guess next to the real breakdown is what an estimate-vs-actual
  calibration is built from.
- `/flowlens:breakdown <backlogId|epicId>` — splits a refined backlog *or*
  one epic into sized, scoped, dependency-ordered tasks, created in one
  bulk call.
- `/flowlens:work <taskId>` — runs the lifecycle above for one task, after
  checking its dependencies are closed.

## Where baseBranch and scope come from

A task's `baseBranch`, `allowedScope` and `forbiddenScope` are never its
own fields. `GET /api/v1/tasks/{taskID}/context` resolves them **epic
first, then backlog, per field**: an epic that overrides only the base
branch still inherits its backlog's scope. Read the context endpoint
rather than the backlog directly — it is the only place that resolution
is applied for you.
