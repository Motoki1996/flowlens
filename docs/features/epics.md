# Epics

## Epics: an optional layer between a backlog and its tasks

A refined backlog is not always broken straight down into
implementation-sized tasks. The work is usually first cut into coarse units —
one screen, one endpoint group, one migration + API pair — and only then is
each of those broken into tasks someone actually works. That coarse unit is
an **epic**.

It is optional in both directions: a task may sit directly in a backlog
exactly as before, and an epic may sit outside any backlog. Nothing about an
existing project changes until someone creates one.

- `GET`/`POST /api/v1/projects/{projectID}/epics`
  (`?backlog_id=`/`?priority=`/`?progress=`/`?assignee=`/`?sort=`) and
  `GET`/`PATCH`/`DELETE /api/v1/epics/{epicID}` — the same route shape,
  bearer-token allowlisting and scopes a backlog's endpoints have.
- The web app's screens are `/projects/[projectId]/epics` (Board / List /
  Timeline view modes, Board by default) and
  `/projects/[projectId]/epics/[epicId]`.
- A task names its epic with `epicId` on create, update and bulk create, and
  the task collections take `?epic_id=<uuid>|unassigned`.

An epic carries the same fields a backlog does — name, description,
start/due dates, priority, progress, assignee, base branch,
allowed/forbidden scope and its own `defaultLinkedGitlabProjectId` — minus
`size`, since an epic's size is just the sum of its tasks', plus
`estimatedPoints` (below). It is app-only:
**no epic is ever created in, or read from, GitLab.** GitLab CE has no Epic
at all (it is a Premium object), which is also why the name was free to take.

A task is linked to an epic from either side, and both write the same
`tasks.epic_id`:

- **From the task** — `epicId` on task create/update, and the Epic control on
  the Task single view.
- **From the epic** — `PATCH /api/v1/epics/{epicID}/tasks` with the epic's
  complete `taskIds` set: what it names is filed under the epic, what it no
  longer names drops out (keeping its backlog). All-or-nothing, so a task from
  another project moves nothing. The web app offers it as a checkbox list of
  the backlog's free tasks, on the epic's create/edit form and on its single
  view.

Two rules the schema can't express are enforced by the API:

- **An epic wins over a backlog.** Naming an `epicId` on a task also files it
  in that epic's backlog, whatever `backlogId` the same request carried.
  Moving a task to a different backlog without naming an epic clears its
  epic, and moving an *epic* between backlogs moves its tasks with it.

  The corollary is worth stating outright, because it is the one case where
  "the epic wins" costs something: an epic that is itself **unfiled**
  (`backlogId` null) unfiles every task filed into it, since the epic's
  backlog — nothing — is written onto the task either way. Filing five of a
  sprint's tasks into a backlog-less epic takes all five out of that sprint.
  The web app refuses to offer the task picker for an unfiled epic for
  exactly this reason; the API does not, so an integration should file the
  epic in a backlog before filing tasks into it.
- **An epic belongs to one project.** An epic in another project's backlog is
  rejected with 400 `invalid_backlog`.

### An epic's provisional estimate (issue #234)

An epic has no `size`, on purpose — an epic's size is the sum of its tasks'.
That is true once the tasks exist, and the point of this rung is that it is
created *before* them: `/flowlens:breakdown-epics` deliberately creates no
tasks at all. In between, an epic weighed **structurally zero**, so
[Velocity](metrics.md#velocity-issue-195)'s forecast — which counted only tasks —
quietly ignored every refined-but-not-yet-broken-down backlog in the project
and reported the remaining work as far smaller than it was.

`estimatedPoints` is the answer, and is deliberately *not* called a size and
not one of the `xs`..`xl` values:

- It is a raw integer on the same scale `internal/velocity` weights sizes
  onto (`xs`=1 … `xl`=8), optional and nullable — `null` means "nobody has
  estimated this epic", which is a different statement from any number. `0`
  is rejected (400 `invalid_estimated_points`, and a CHECK constraint) for
  exactly that reason: allowing it would make "no work" and "no estimate"
  indistinguishable.
- **It loses authority the moment the epic has tasks.** The rule lives in
  one place, `internal/epic.EffectivePoints`: tasks' summed sizes if there
  are any, the estimate if not, *unknown* if neither — never silently 0.
  A separate name and unit is what keeps this from becoming the "two
  disagreeing truths" an epic-level `size` would be.
- **It is never cleared or overwritten when the tasks appear.** The original
  guess sitting beside the eventual real breakdown is the only data an
  estimate-vs-actual calibration could ever be built from.

It is set with `estimatedPoints` on `POST /api/v1/projects/{projectID}/epics`
and `PATCH /api/v1/epics/{epicID}` (absent keeps the stored value, explicit
`null` clears it), and `/flowlens:breakdown-epics` fills it in by projecting
how many tasks of which sizes the epic will become from the project's own
existing task sizes — never from a bare guess, which could not be calibrated
against anything later.

Backlogs deliberately get no estimate of their own: a backlog's is the sum of
its epics'.

Deleting an epic never deletes its tasks: they drop back to sitting directly
in their backlog, exactly where they were before the epic existed. Abandoning
the rung costs nothing.

See [ADR-0012](../decisions/0012-why-an-epic-layer.md) for why this is a
separate object rather than a parent task, and why it stays out of GitLab.

## An epic's base branch and change scope

This is what the rung mainly exists for. A backlog is often too coarse to
name one branch: two coarse units inside it routinely target different
release branches, and the only way to say so used to be splitting the
backlog — which loses the grouping the backlog was there to provide.

An epic therefore carries its own `baseBranch`, `allowedScope` and
`forbiddenScope`, with the same validation the backlog's have. They are
resolved into `GET /api/v1/tasks/{taskID}/context` **epic first, then
backlog, per field**:

| Epic | Backlog | Task context reads |
| --- | --- | --- |
| `release/2.4` | `main` | `release/2.4` |
| `""` | `main` | `main` |
| `""` | `""` | `""` |

Per field, not per object: an epic that overrides only `baseBranch` still
inherits its backlog's scope. The Epic single view says which of the two each
value came from, since only one of them follows the backlog when it changes.

The issue destination for a *new* task gains the same rung: the task's epic's
link, then its backlog's, then the project's default, then nowhere at all.
Like the rungs below it, that is read **only when the task is created** —
`task_gitlab_links` governs every later update, so moving a task between
epics never moves or re-targets an issue that already exists.

## A task carries its epic with it

`GET /api/v1/tasks/{taskID}` embeds its task's epic as an `epic` object, so a
caller holding nothing but a task id sees the rung above it — its base branch
above all — without a second round trip:

```json
{
  "id": "…", "title": "Build it", "epicId": "…",
  "epic": {
    "id": "…", "name": "Screens", "description": "…",
    "startDate": null, "dueOn": null,
    "priority": "high", "progress": "in_progress",
    "baseBranch": "release/2.4", "allowedScope": "", "forbiddenScope": "",
    "estimatedPoints": 13
  }
}
```

Three things it deliberately is not:

- **Not resolved.** These are the epic's *own* values, empty where the epic
  sets nothing — `"allowedScope": ""` above does not mean the task may touch
  nothing, it means the backlog's applies.
  `GET /api/v1/tasks/{taskID}/context` remains the one place that answers
  "what applies to this task", already resolved epic-then-backlog per field,
  and stays what an agent should follow.
- **Not the whole epic.** No `projectId`/`backlogId` (both already on the
  task), no assignee names, no task counts. `GET /api/v1/epics/{epicID}` is
  still there for those.
- **Not on the list endpoints.** A collection carries `epicId` alone and
  sends `"epic": null`; only the single-task read pays for the lookup. The
  key is always present, so a client never has to distinguish absent from
  null. It is also null when the task simply has no epic.

