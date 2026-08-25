# 0012. Why an Epic layer between Backlog and Task

- **Status:** Accepted
- **Date:** 2026-08-25

## Context

FlowLens's hierarchy was `Project > Backlog > Task`, and a refined backlog was
broken straight down into implementation-sized tasks — the job
`/flowlens:breakdown` in `@motokis-lab/agent-kit` does in one step.

That is not how the work actually decomposes. After a backlog is refined into
numbered requirements, the next move is to cut it into coarse units — one
screen, one endpoint group, one migration+API pair — and only then break each
coarse unit into tasks an agent or a human works one at a time. The coarse
unit had nowhere to live, so it became one of two bad things:

- **A task with no code behind it.** `internal/velocity` counts it, the Board
  draws it beside its own children, and its `size` is meaningless.
- **A numbered requirement inside the backlog's description.** Free text
  cannot carry a base branch, an assignee, a due date or a change scope.

The base branch is the sharpest version of the problem. `backlogs.base_branch`
([000024](../../apps/api/migrations/000024_backlog_base_branch.up.sql)) exists
because an agent working a task has to know what to branch from. But a backlog
is often too coarse to name one branch: two coarse units inside the same
backlog routinely target different release branches, and the only way to
express that was to split the backlog — which then loses the grouping the
backlog existed to provide.

## Decision

Add `Epic`: an **optional** rung between a backlog and its tasks, app-only,
carrying its own `base_branch`, `allowed_scope`/`forbidden_scope`, priority,
progress, dates, assignee and issue-destination link.

Optional in both directions — a task may sit directly in a backlog exactly as
before (`tasks.epic_id` NULL), and an epic may sit outside any backlog
(`epics.backlog_id` NULL). Nothing about an existing project changes until
someone creates an epic.

### 1. Why the name `Epic`

It is the standard vocabulary for this rung, so it needs no explanation, and
**GitLab CE has no Epic** — it is a Premium object — so the name cannot
collide with anything on the sync surface. `Milestone` was rejected for the
opposite reason: it *does* exist in CE, and a future milestone sync would then
have two meanings for one word. `Feature` is closer to how the coarse unit is
usually described ("画面単位") but sits awkwardly beside FlowLens's otherwise
GitLab-derived vocabulary.

### 2. Why a new table, not `tasks.parent_task_id`

A self-referencing parent task would have been much less code: it reuses every
task query, screen and sync path, and the coarse unit would get a real GitLab
issue for free. It was rejected because it corrupts the things FlowLens
measures:

- `internal/velocity` would count the parent *and* its children, double-counting
  every piece of work — the exact inflation the `size` weighting exists to
  prevent.
- The Board would draw a parent in a progress column beside its own children.
- `base_branch` would have to become a `Task` column, contradicting its own
  definition: the branch a *group* of tasks starts from.

Keeping `Task` as one object with one meaning is worth the extra table.

### 3. Why an epic is shaped like a backlog

Every column on `epics` already exists on `backlogs` with the same meaning.
That is deliberate, not accidental duplication: it lets one resolution chain
walk both rungs, and lets the web app's Backlog collection sections be
generalised over the two objects instead of copied. The shared field rules
live in `internal/fieldnorm` so the two packages cannot drift from each other
or from the CHECK constraints they share.

The one field an epic deliberately does **not** get is `size`. An epic's size
is the sum of its tasks', the same reason a backlog has none.

An epic also gets no progress-events table of its own. `backlog_progress_events`
exists to feed flow metrics; an epic is not measured, so there is nothing to
record.

### 4. Why app-only, with no GitLab issue

No epic is created in, or read from, GitLab. Creating a parent issue per epic
would inflate the project's issue count with items at a granularity the team
does not work at, and would give `internal/mrsync`'s branch/description
matching a second issue per unit of work to confuse a merge request with.

The epic does carry `default_linked_gitlab_project_id`, which participates in
the **task's** issue destination only: epic's link → backlog's link → project
default → nowhere. Like the rungs below it, that is read **only at task-create
time**; `task_gitlab_links` governs every later update, so moving a task
between epics never moves or re-targets an issue that already exists.

### 5. Why consistency is enforced in the application layer

Two rules cannot be expressed in the schema:

- A task's `epic_id` and `backlog_id` must agree — the task's epic must belong
  to the task's backlog.
- An epic's `backlog_id` and its own project must agree.

`internal/epic` and `internal/task` enforce them, writing `backlog_id` from
the epic in the same statement and moving an epic's tasks with it when the
epic changes backlog. This is the same place `internal/backlog`'s
"link must belong to this project's connection" and `internal/taskdependency`'s
cycle check already live.

## Consequences

- One more rung to teach: the agent kit gains `/flowlens:breakdown-epics`
  (backlog → epics) alongside `/flowlens:breakdown` (→ tasks), and the
  epic-first resolution of `baseBranch`/scope becomes something an agent must
  know about.
- `GET /api/v1/tasks/{taskID}/context` resolves per field, not per object: an
  epic that sets only `base_branch` still inherits its backlog's scope. A
  reader of one field cannot infer where the others came from.
- Deleting an epic unfiles its tasks (`ON DELETE SET NULL`), returning them to
  sitting directly in their backlog — exactly where they were before the epic
  existed. Nothing is ever lost by abandoning the rung.
- Velocity and delivery metrics are unchanged: they still count tasks. An epic
  is a grouping, not a unit of throughput.
