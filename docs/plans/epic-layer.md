# Plan: `Epic` — an optional layer between `Backlog` and `Task`

**Status:** agreed, not started.

## Why

Today a project's hierarchy is `Project > Backlog > Task`, and a refined
backlog is broken straight down into implementation-sized tasks. In practice
there is a step in between: after a backlog is refined, the work is first cut
into coarse units — one screen, one endpoint group, one migration+API pair —
and only then is each coarse unit broken into tasks that an agent or a human
actually works.

That coarse unit currently has nowhere to live. It ends up either as a task
with no code behind it (which pollutes velocity and the Board) or as a
numbered requirement inside the backlog's description (which cannot carry a
base branch, an assignee, or dates).

`Epic` is that layer. It is **optional**: a task may sit directly in a
backlog, exactly as today, or in an epic inside a backlog.

This plan contradicts the permanent docs until it ships — [`README.md`](../../README.md),
[`docs/ui-design.md`](../ui-design.md) and [`CLAUDE.md`](../../CLAUDE.md) all
describe a two-level `Backlog > Task` hierarchy. Phase 7 folds this in and
deletes the plan.

## Decisions taken

| Decision | Choice | Rationale |
| --- | --- | --- |
| Name | `Epic` | The standard vocabulary for this rung, and GitLab **CE** has no Epic (it is a Premium object), so nothing in the sync surface collides. `Milestone` was rejected precisely because it *does* exist in CE and a future milestone sync would collide. |
| Modelling | New `epics` table + `tasks.epic_id` | Keeps `Task` one object with one meaning. A self-referencing `tasks.parent_task_id` was rejected: a parent task would be counted by `internal/velocity` and drawn on the Board alongside its own children, and `base_branch` on a `Task` contradicts "the branch a *group* of tasks starts from". |
| GitLab | App-only, never synced | Same posture as `Backlog`. No epic-level issue is created, nothing is read back. An epic does carry `default_linked_gitlab_project_id`, which only takes part in the *task's* issue-destination resolution. |
| Membership in a backlog | `backlog_id` nullable, `ON DELETE SET NULL` | Mirrors `tasks.backlog_id` exactly: deleting a backlog must not delete its epics, it unfiles them. |

An ADR (`docs/decisions/0012-why-an-epic-layer.md`) is written in phase 1,
covering the modelling and app-only choices above — they outlive the plan.

## Object shape

`epics`, deliberately shaped as "a `Backlog` that lives inside a backlog" —
every field below already exists on `backlogs` with the same meaning, which
is what keeps the resolution chains and the UI components reusable:

| Column | Notes |
| --- | --- |
| `id`, `project_id`, `created_at`, `updated_at` | as `backlogs` |
| `backlog_id UUID REFERENCES backlogs(id) ON DELETE SET NULL` | NULL = unfiled epic, the `Unclassified` group, mirroring `tasks.backlog_id` |
| `name TEXT NOT NULL`, `description TEXT NOT NULL DEFAULT ''` | description rendered as Markdown by `components/Markdown.tsx` |
| `position INTEGER NOT NULL DEFAULT 0` | manual order, bulk-reorderable |
| `start_date DATE`, `due_on DATE` | Timeline view |
| `priority TEXT NOT NULL DEFAULT 'medium'` | same CHECK set as `backlogs` |
| `progress TEXT NOT NULL DEFAULT 'not_started'` | same CHECK set; Board axis |
| `assignee_user_id UUID REFERENCES users(id) ON DELETE SET NULL` | app-only, no GitLab bridge (a backlog's rule, not a task's) |
| `base_branch TEXT NOT NULL DEFAULT ''` | **the field this feature exists for** |
| `allowed_scope`, `forbidden_scope TEXT NOT NULL DEFAULT ''` | same shape as `backlogs` (000029) |
| `default_linked_gitlab_project_id UUID REFERENCES linked_gitlab_projects(id) ON DELETE SET NULL` | must belong to the project's own connection, checked in `internal/epic` like `internal/backlog.validateLink` |

And `tasks.epic_id UUID REFERENCES epics(id) ON DELETE SET NULL`, indexed as
`(project_id, epic_id)`.

There is deliberately **no `size`** on an epic, for the same reason a backlog
has none: an epic's size is the sum of its tasks'.

### Resolution chains

Two chains gain a rung, both **epic first, backlog second**:

1. **Agent defaults** (`GET /api/v1/tasks/{taskID}/context` — `baseBranch`,
   `allowedScope`, `forbiddenScope`): task's epic → task's backlog → `""`.
   Resolved per field, not per object: an epic that sets only `base_branch`
   still inherits the backlog's scope fields. This replaces
   `GetBacklogTaskDefaults` with a single query joining both.
2. **Issue destination** (`internal/task.Service.Create` only): task's epic's
   link → task's backlog's link → project default link → nowhere. Unchanged
   in every other respect — it is still read *only* at create time, and
   `task_gitlab_links` still governs every later update, so moving a task
   between epics never moves or re-targets an issue.

### Consistency rules (application layer, not schema)

The schema cannot express these; `internal/epic` and `internal/task` enforce
them, alongside the existing cross-table rules:

- A task's `epic_id` and `backlog_id` must agree: setting a task's epic sets
  its `backlog_id` to that epic's `backlog_id` in the same write. Moving a
  task to a different backlog clears `epic_id` unless the epic given in the
  same request belongs to the new backlog.
- An epic's `backlog_id` and its tasks' must agree: moving an epic between
  backlogs moves its tasks with it (one transaction).
- An epic and its `backlog_id`/`default_linked_gitlab_project_id`/
  `assignee_user_id` must all belong to the same project.

## Phases

Each phase is a PR that leaves `main` shippable. `make test`, `make lint` and
`make generate` after every schema or query change.

### Phase 1 — schema + domain

- ADR `docs/decisions/0012-why-an-epic-layer.md`.
- Migration `000032_epics.up/.down.sql`: the table above, `tasks.epic_id`,
  indexes `idx_epics_project_id`, `idx_epics_project_id_backlog_id`,
  `idx_epics_project_id_assignee_user_id`, `idx_tasks_project_id_epic_id`,
  and the partial index on `default_linked_gitlab_project_id` that 000021
  added to `backlogs`. All columns nullable or defaulted, no backfill needed
  — a pre-existing task reads as "no epic", which is the intended meaning.
- `internal/database/queries/epics.sql` modelled on `backlogs.sql`
  (`CreateEpic`, `ListEpicsByBacklog`/`ByProject` with the task-count
  aggregate, `GetEpicForOwner`, `GetEpicProjectID`, `ReorderEpics`,
  `UpdateEpicForOwner`, `DeleteEpicForOwner`), plus a replacement for
  `GetBacklogTaskDefaults` that resolves per field across both rungs.
- `internal/epic`: `Service` with `Create/List/Get/ProjectID/Update/Reorder/
  Delete`, the same ownership-through-project posture as `internal/backlog`,
  reusing `internal/assignee` and `internal/optional`. Domain tests
  (table-driven, Fakes) per [`docs/testing.md`](../testing.md).

### Phase 2 — task wiring

- `internal/task`: `epicId` on create/update/bulk-create/context, the two
  resolution chains above, and the epic/backlog consistency rules.
- `epicId` is **app-only**: it must not appear in `mirroredFieldsChanged`, so
  changing it alone never enqueues an `issue.update`.
- `?epic=<uuid>|none` on both task collections (project-scoped and
  cross-project), alongside the existing `?backlog=`.
- Integration test covering: create in epic → issue lands in the epic's
  linked project; move between epics → no `issue.update` enqueued.

### Phase 3 — HTTP + OpenAPI

- Routes, mirroring the backlog block in `internal/http/server.go` exactly
  (bearer-allowlisted, `requireTokenProjectMatch` / a
  `requireTokenResourceProject("epicID", …)` resource guard):
  - `GET|POST /api/v1/projects/{projectID}/epics` (`?backlogId=`, `?priority=`,
    `?progress=`, `?assignee=`, `?sort=`)
  - `PATCH /api/v1/projects/{projectID}/epics/order`
  - `GET|PATCH|DELETE /api/v1/epics/{epicID}`
- `openapi/paths/epics.yaml` + schema components in the same PR —
  `internal/http/openapi_drift_test.go` fails otherwise.
- Handler tests for the permission branches and the 400 mapping of each
  sentinel error.

### Phase 4 — web: the `Epic` object's own screens

Object-first per [`docs/ui-design.md`](../ui-design.md):

- `/projects/[projectId]/epics` — collection, Board (default, axis progress) /
  List / Timeline view modes, reusing `BacklogBoardSection`,
  `BacklogListSection`, `BacklogTimelineSection` and `GanttChart` by
  generalising them over the two objects rather than copying.
- `/projects/[projectId]/epics/[epicId]` — single view, inline editing, an
  "Epic" section on the Task collection filter, and a "Tasks" card linking to
  `/projects/[projectId]/tasks?epic=…`.
- Add `Epics` to the project section nav between `Backlogs` and `Tasks`, and
  the breadcrumb rung `Backlogs / <backlog> / Epics / <epic>`.
- Storybook stories per [`docs/storybook.md`](../storybook.md); one story per
  permission/data branch.

### Phase 5 — web: the surrounding screens

- `Backlog` single view gains an "Epics" card (count + completion ratio),
  the way it already shows tasks.
- `Task` single view shows its epic, and the task edit form picks an epic
  filtered to the task's backlog.
- Dashboard and the cross-project Task collection show the epic name on a task
  row where the backlog name already appears.

### Phase 6 — agent kit

Two breakdown commands, **keyed by the layer they create**, not by the layer
they read:

- **New** `/flowlens:breakdown-epics <backlogId>` — reads a refined backlog
  (`R1`…`Rn`) and creates **epics** under it, each carrying its own
  `baseBranch`, `allowedScope`/`forbiddenScope`, `priority`, dates, and a
  description that records which `R#` it covers. It creates nothing below
  that rung.
- **Existing** `/flowlens:breakdown <backlogId|epicId>` — still creates
  **tasks**, and gains the epic argument. It resolves the argument by
  `GET /api/v1/epics/$1` first, falling back to `GET /api/v1/backlogs/$1` on
  404, then files the tasks under whichever object it found.

Why two commands rather than one two-step command: the epic rung is optional,
so `backlog → tasks` must stay a single call for a project that doesn't use
epics — which is exactly what the unchanged `/flowlens:breakdown <backlogId>`
path is. Keeping the existing name also means an already-installed
`.claude/commands/flowlens/breakdown.md` keeps working after an `init` refresh.

Also in this phase:

- No `refine-epic` command. `breakdown-epics` writes each epic's description,
  `baseBranch` and scope at creation time, so there is nothing left for a
  separate refine step; `refine-backlog` is unchanged.
- `breakdown` learns that `allowedScope`/`forbiddenScope`/`baseBranch` are
  resolved **epic-first** (see "Resolution chains"), so when it is given an
  epic it must `PATCH` the *epic*, not the backlog, if the scope needs fixing.
- `SKILL.md`: the call order becomes `refine-backlog` → `breakdown-epics` →
  `breakdown` → `work`, with the epic rung marked optional, and the command
  list gains the new entry.
- `packages/agent-kit/README.md`'s installed-files table gains
  `.claude/commands/flowlens/breakdown-epics.md`.
- Epics are created with one `POST /api/v1/projects/{projectID}/epics` per
  epic — deliberately **no** `epics/bulk` endpoint. `tasks/bulk` exists
  because tasks carry a dependency graph that has to be written atomically;
  epics have no dependencies between them, so a partial failure is a
  re-runnable annoyance, not a corrupt graph.
- Bump the package version; `init` rewrites `.flowlens/openapi.yaml` anyway.

### Phase 7 — docs, then delete this plan

- `README.md`: an "Epics" section, and the resolution chains updated wherever
  "backlog's link → project's default link" appears.
- `docs/ui-design.md`: `Epic` in the object table and the route table.
- `CLAUDE.md`: the hierarchy sentence and the base-branch/scope paragraphs.
- `docs/self-hosting.md`: nothing to add (no new env var), but confirm the
  migration is covered by the documented `docker compose pull && up -d`.
- Delete `docs/plans/epic-layer.md` and its entry in `docs/plans/README.md`.

## Explicitly out of scope

- Epic-level velocity or delivery metrics. `internal/velocity` keeps counting
  tasks; an epic is a grouping, not a unit of throughput.
- Epic-level progress derived from its tasks. An epic's `progress` is its own,
  exactly as a backlog's is (000011).
- Any epic↔GitLab sync, in either direction.
- Nesting epics inside epics.
