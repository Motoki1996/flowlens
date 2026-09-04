# Tasks, backlogs and their fields

How FlowLens's core objects behave: a task's dates and dependencies,
the priority / progress / size / assignee axes it and a backlog share,
closing a backlog or an epic, and creating tasks in bulk.

## Task & backlog scheduling, Gantt charts

Beyond GitLab issue sync, a task can carry a `startDate` (alongside the
existing `dueOn`) and predecessor/successor dependencies on other tasks in
the same project, and a **backlog** carries its own `startDate`/`dueOn` pair.
All of these are app-only: GitLab Issues have no native start-date or
dependency concept and a backlog is not a GitLab milestone, so none of them is
ever pushed to or pulled from GitLab — a task's `dueOn` remains the only date
field synced with the linked GitLab issue.

- `PATCH /api/v1/tasks/{taskID}` accepts `startDate` alongside the task's
  other mirrored fields. It is a **partial** update: a key absent from the
  body leaves that field alone, and an explicit `null` clears a nullable one
  (backlog, assignee ID, either date). That is what lets a client edit one
  attribute without echoing the whole task back. An edit that touches only
  app-only fields (`startDate`, backlog) enqueues no `issue.update` job.
- `POST /api/v1/projects/{projectID}/task-dependencies` records that
  `predecessorTaskId` must finish before `successorTaskId` starts. Both
  tasks must belong to the project, and the edge is rejected with 409 if it
  would close a cycle (checked in the application layer via a reachability
  walk, since neither a `CHECK` nor a `UNIQUE` constraint can express "no
  cycles").
- `GET /api/v1/projects/{projectID}/task-dependencies` lists every
  dependency in the project; `DELETE /api/v1/task-dependencies/{id}` removes
  one.
- `POST /api/v1/projects/{projectID}/backlogs` and
  `PATCH /api/v1/backlogs/{backlogID}` accept `startDate` and `dueOn`. On the
  PATCH the two dates are **partial** in the same sense as a task's: absent
  leaves the stored value alone, an explicit `null` clears it. That is what
  keeps a rename — which sends only name and description — from
  wiping the backlog's planned period. A period whose start is after its due
  date is rejected with 400 `invalid_schedule`.

In the web app, a task is created from the "New task" action on the project's
Task collection (`/projects/{projectId}/tasks`), which takes its title,
description, backlog and both dates up front. It is edited from the "Edit task"
action on the task's own single view (`/projects/{projectId}/tasks/{taskId}`),
which swaps the attribute block for an inline form covering title,
description, assignee, labels, backlog and both dates — there is no separate
edit screen, since editing is an action on the Task object (see
[`docs/ui-design.md`](../ui-design.md), rule 4). Predecessors and successors
are added and removed from a "Dependencies" section on the same screen; a
rejected cycle is reported there. The same collection has a
"Timeline" view mode (alongside "List" and the default "Board" mode, per the OOUI rule that
a collection is one dataset presented several ways) that lays out scheduled
tasks as a Gantt chart. It is built on the shadcn `chart` component (Recharts)
so it inherits the same tokens and tooltip styling as the rest of the UI, and is
loaded on demand — the charting library stays out of the collection's bundle
until someone switches to the Timeline:

- Bars are a stacked horizontal bar chart: a transparent leading segment
  positions each task at its start date, and the visible segment spans
  start → due inclusive. A task with only one of the two dates occupies that
  single day.
- The plotted range always covers every scheduled task, so the chart never hides
  data. How much detail that range is read at is the reader's choice: a **Zoom**
  control (Quarter / Month / Week / Day) sets both the width a day gets and the
  axis tick interval, and the plot scrolls horizontally rather than compressing
  bars into slivers. The initial level is derived from the span — daily ticks up
  to three weeks, then weekly, then monthly, then quarterly past about eighteen
  months — so a short sprint and a multi-year roadmap are each legible without
  touching the control. Quarter is deliberately the coarsest level: it is the
  unit a roadmap is planned in, and the last one at which a bar is still a bar.
- The two coarse levels rule the plot at the interval below their labels —
  weeks under monthly labels, months under quarterly ones — drawn fainter and
  unlabelled, because "Aug 2026" spans thirty-odd days of plot and a bar has to
  be placeable against something narrower than that. Day zoom instead shades
  **weekends**, which is where the question becomes "how many working days is
  that?"; at any coarser level the bands would stripe the whole plot into noise.
- A bar is never drawn narrower than 6px, so a one-day task stays visible at
  quarter zoom (where a day is 1.6px) instead of reading as a task nobody
  scheduled. The floor applies to the whole bar and is then split at the
  completion ratio, so a part-done backlog keeps its fill.
- "Today" is drawn as a reference line when it falls in range, a **Today** button
  scrolls the plot back to it (disabled when today is outside the range), and a
  long project opens scrolled to today rather than to its earliest date. Changing
  the zoom magnifies around whatever was on screen instead of jumping to the
  start.
- Zoom and scroll position are local view state, exactly like the view-mode
  toggle they sit beside: they are not persisted to the URL or sent to the API,
  because the timeline redraws a collection that has already been fetched.
- A bar's colour is a status, never an identity: open work is the brand hue,
  an open task past its due date is destructive-red, and closed work recedes to
  muted. A legend names all three, so colour is never the only cue.
- Task names sit in a column beside the plot rather than as axis labels, so each
  one stays a real link to the task's single view; the bars themselves are also
  clickable. The trade is a fixed-width column that truncates long names, so the
  divider between the two is a **splitter**: drag it (or focus it and use the
  arrow keys) to give names more room, double-click to hand the width back to
  the viewport. Like zoom, it is local view state. A name the column has
  actually clipped also states itself in full on hover or keyboard focus — a
  real tooltip rather than the browser's `title`, which took about a second to
  appear; it is gated on the name being clipped, since one repeating a title
  already on screen is noise.
- Gridlines are mixed from the same `--muted-foreground` the axis labels use,
  not from `--border`: on this theme the border sits a couple of steps off the
  card surface, and the shadcn chart default drew a grid nobody could see. The
  minor lines are half the strength of the major ones, which is what makes them
  read as a subdivision rather than as a second grid.
- Alternate rows are shaded, in the name column and the plot alike, so one
  continuous band ties a name to its bar — on a chart wide enough to scroll,
  the two are far apart and the tooltip cursor only ever highlights the one row
  the pointer is on.
- That column belongs to the name: the title has the line to itself, and only a
  **high or urgent** priority appears under it, as the same badge the list rows
  use. Every priority and a task's progress are stated on the bar's tooltip
  instead — they are what the Board and List modes are read for, and as two pills
  *beside* the title they left it a few dozen pixels, so every row read as an
  ellipsis.
- A project-wide closed/total progress ratio sits above the chart, and each
  task's predecessors are noted under its name. Tasks with neither a start date
  nor a due date are listed separately below the chart rather than silently
  dropped.

The Backlog collection (`/projects/{projectId}/backlogs`) has the same
Timeline mode alongside its Board and List modes, drawing one bar per
scheduled backlog with the same axis, zoom and Today controls, colours and
today marker. What it adds is **completion**: each bar is filled by
the share of that backlog's tasks that are closed, with the remainder drawn in
the same hue at low opacity, so plan and progress are read in one place. The
ratio is also stated as text (`3/8 closed (38%)`) beside the bar, in the
tooltip, and on the backlog's single view — the fill is a second reading of it,
never the only one. A backlog with no tasks reads "No tasks" and stays unfilled
rather than appearing complete. Unlike the Task collection's Timeline, none of
this reads a task list: `GET .../backlogs` (issue #144) returns each backlog's
own `taskCount`/`closedTaskCount`, computed by a `LEFT JOIN` aggregate in the
same query, so the List row count, the Board card's ratio and the Timeline
bar's fill all come straight off the backlog object the collection already
fetched — the screen never has to fetch every task in the project just to
derive them. The backlog's single view is the exception: it already fetches
that one backlog's own tasks to list them, and derives its completion ratio
from that list instead, so a failed fetch there still reports the ratio as
unavailable rather than 0%.

The date math lives in `apps/web/lib/timeline.ts`, separate from the components
so it is unit-testable without rendering a chart; the zoom level and scroll
position are owned by the `useTimelineViewport` hook beside it, shared by both
timelines. The layout they share — the name column, the splitter, the row
banding and the scrolling plot — is `components/TimelineFrame.tsx`, which the
Task and the Backlog/Epic sections differ inside only by what sits under each
name (a predecessor list, or a completion ratio).

## Task & backlog priority

A task and a backlog each carry a `priority` — one of `low`, `medium`, `high`,
`urgent`, defaulting to `medium` — used to decide what to work on next, ahead
of the delivery-flow dashboard planned later and the
[cross-project task collection](views.md#cross-project-task-collection) built on it
today. Like `startDate`, task dependencies and a backlog's own dates, priority is
**app-only and never synced to GitLab**: GitLab CE issues have no native
priority field (a priority label or weight is a GitLab EE feature), so it has
no GitLab-side counterpart to push to or pull from.

- `POST`/`PATCH` on both `/api/v1/projects/{projectID}/tasks` /
  `/api/v1/tasks/{taskID}` and `/api/v1/projects/{projectID}/backlogs` /
  `/api/v1/backlogs/{backlogID}` accept `priority`. On the task PATCH,
  priority is part of the same partial-update contract as the rest of the
  task: a body without `priority` leaves the stored value alone. An absent
  `priority` on create, or an explicit empty string on either create or
  update, resets it to `medium` rather than erroring — there is no "no
  priority" state to represent. Any other value is rejected with 400
  `invalid_priority`.
- `GET .../tasks` and `GET .../backlogs` accept `?priority=low|medium|high|urgent`
  to narrow the list, and `?sort=priority` to order results by priority
  (`urgent` → `low`) instead of the default creation order, falling back to
  that same creation order to break ties between equal priorities. Sorting by
  priority is a display order for this request only and is never stored.
  The project-scoped task list also accepts `?sort=dueOn` (due date
  ascending, tasks with no due date last) and `?sort=updatedAt` (most recently
  updated first) — the same three values as the cross-project collection, so
  a screen's sort menu means one thing whichever list backs it. Backlogs take
  `?sort=priority` and `?sort=progress` only.

In the web app, priority is selectable wherever a task or backlog is created
or edited: the task single view's edit form, the task collection's inline
"New task" form, and — since a backlog's own rename/edit action already lives
on the Backlog collection view, not its single view, per
[`docs/ui-design.md`](../ui-design.md) — the backlog collection's inline
"New backlog" and per-row "Edit" forms. It is shown as a badge, the same
component for both tasks and backlogs, in list rows and the task single view.
On the timeline the badge sits under the title rather than beside it, and only
for `high`/`urgent`; the bar's tooltip states every priority — see
[the Gantt charts above](#task--backlog-scheduling-gantt-charts). A backlog's priority is independent of its tasks':
creating or editing one never reads or writes the other. Priority is no longer
the board's axis — see [Task & backlog progress](#task--backlog-progress)
below — but it stays a badge on every board card.

## Task & backlog progress

A task and a backlog each also carry a `progress` — one of `not_started`,
`in_progress`, `on_hold`, `done`, defaulting to `not_started` — FlowLens's own
four-stage record of how far the work has got.

It is deliberately **not** a task's `status`. That field is the GitLab issue
state (`open`/`closed`) and is kept in sync both ways; `progress` is app-only
and never synced to GitLab, like `priority`, `startDate` and task
dependencies. The two never write each other: closing a task on either side
leaves its progress alone, and moving a task to `done` never closes its
GitLab issue. A task can legitimately read *Closed* and *On hold* at once, and
both badges are shown wherever either is.

**Progress sync on issue close is the one, opt-in exception** (issue #202).
A spec-driven flow ends with a merge, not a manual progress edit, so left as
is a merged task sits at whatever progress it had until a human notices —
which the Board, dashboard and velocity all read from. A project can turn on
a per-project setting so that closing a task's linked GitLab issue *also*
moves its progress to `done`:

- `PUT`/`GET /api/v1/projects/{projectID}/progress-sync-settings` (owner-only,
  `{enabled}`, off by default) — the same owner-only, always-exists-with-a-
  default shape as [notification settings](teams.md#notification-digest-issue-109).
- The write only ever moves progress *to* `done`, and only once: it fires on
  a genuine `open`→`closed` transition of the task's status (never on a
  redelivered or re-applied "already closed" update, so a duplicate webhook
  never appends a second event), and never if progress is already `done`.
  Reopening the issue (`closed`→`open`) never reverts it — the sync is
  one-directional. If a human has since moved progress off `done`, a stale
  re-apply of the same close will not put it back, since the transition
  already happened once.
  Both inbound paths apply it — a live `Issue Hook` webhook
  (`internal/webhookapply`) and the periodic `project.resync`/`project.import`
  walk (`internal/projectsync`) — and both write it atomically with the
  status change that triggered it, so a crash between the two never leaves
  one without the other.
- The change is recorded as a `task_progress_events` row with
  `actor_kind = "gitlab"` (`internal/task`'s `ActorKindGitlab`), the third
  value alongside `"user"`/`"agent"` (issue #169) — an audit trail entry with
  no acting user (`actor_user_id` is `NULL`), the same as `"agent"`.
  [Velocity](metrics.md#velocity-issue-195)'s user/agent/unknown actor split does not
  yet have its own `"gitlab"` bucket and currently counts these under
  "unknown" — left as a known gap, not implemented as part of issue #202.

- `POST`/`PATCH` on both `/api/v1/projects/{projectID}/tasks` /
  `/api/v1/tasks/{taskID}` and `/api/v1/projects/{projectID}/backlogs` /
  `/api/v1/backlogs/{backlogID}` accept `progress`, under the same
  partial-update contract as `priority`: a task PATCH without `progress`
  leaves the stored value alone, and an absent or explicitly empty value on
  create resets it to `not_started` rather than erroring. Any other value is
  rejected with 400 `invalid_progress`.
- `GET .../tasks` and `GET .../backlogs` accept
  `?progress=not_started|in_progress|on_hold|done` to narrow the list, and
  `?sort=progress` to order by progress. Progress ranks the **opposite** way
  from priority — `not_started` first through `done` — so the order reads as
  the work advancing and matches the board's left-to-right axis. Like
  `?sort=priority` it is a display order for the request only. The
  cross-project collection `GET /api/v1/tasks`
  accepts both parameters too.

In the web app, progress is selectable everywhere priority is (both create
forms, both edit forms), and shown as its own badge beside the priority badge
in list rows and the single views. On the timeline it is stated on the bar's
tooltip rather than in the name column, for the same reason as priority above.
A backlog's progress
is its own, set by hand — it is *not* derived from its tasks, and is separate
from the closed/total task ratio the backlog board and timeline also show.

### The Board view mode

Both collections **present** progress as a "Board" view mode (alongside List
and Timeline, per the OOUI rule that a collection is one dataset presented
several ways): one column per stage — Not started, In progress, On hold, Done,
left to right, so the axis reads as the work advancing — with a card per object
stacked inside its column. The columns and their accents come from
`apps/web/lib/progress.ts`, so the two boards can never disagree on which way
the axis points.

- **Backlog board** (`/projects/{projectId}/backlogs`, the collection's
  *default* mode): each card shows the backlog's planned period, its priority
  badge, and its closed/total task ratio, with the ratio drawn as a fill and
  stated as text.
- **Task board** (`/projects/{projectId}/tasks`, the *default* mode here too):
  each card names the task's backlog (or Unclassified), its due date
  and assignee, its labels, and its priority, status and sync badges — the
  board's axis is progress, so neither a closed task nor an urgent one may be
  read off the column it sits in. It renders the same filtered and sorted set
  the List and Timeline modes do, so
  `?q=`/`?status=`/`?progress=`/`?backlog=`/`?sort=` narrow every mode
  together.

Dragging a card to another column changes that object's progress through the
same `PATCH /api/v1/backlogs/{backlogID}` / `PATCH /api/v1/tasks/{taskID}`,
applied optimistically and rolled back with an error if the request fails.
Dragging is the only way the board changes progress; the object's own edit form
remains the keyboard path. Everything else stays in the List mode — creating,
editing, deleting, and (for tasks) moving between backlogs — since the
board's one axis is progress.

## Task size

A task carries a `size` — one of `xs`, `s`, `m`, `l`, `xl`, defaulting to
`m`, the exact middle of the five — a coarse estimate of how much work it is.
Its purpose is to weight [Velocity](metrics.md#velocity-issue-195): a raw completed-task
count can be inflated for free by splitting tasks smaller, and size is what
lets throughput measure the work finished rather than merely the items
finished. Like `priority`, it is **app-only and never synced to GitLab**
(GitLab CE issues have no size field; weight is an EE feature), and an issue
imported from GitLab starts at `m` for a human to size afterwards.

**This is deliberately not a story-point field.** Issue #195 rejected
estimates on the grounds that they rot when a human has to re-enter a number
on every task, and that reasoning still stands: `size` is a five-value
T-shirt scale, and the numeric weights it maps to —
`xs`=1, `s`=2, `m`=3, `l`=5, `xl`=8 — live in
`apps/api/internal/velocity`, not in the schema and not in anyone's typing.
The steps are Fibonacci-ish rather than linear because uncertainty grows
faster than size does. There is still no sprint/timebox concept, and no
"points per sprint" figure.

**Backlogs deliberately have no size**, unlike `priority` and `progress`
which both objects carry: a backlog's priority is genuinely independent of
its tasks', but its size would just be the sum of theirs, and a hand-entered
one could only ever contradict them.

- `POST`/`PATCH` on `/api/v1/projects/{projectID}/tasks` and
  `/api/v1/tasks/{taskID}` accept `size`, under the same partial-update
  contract as the rest of the task: a body without `size` leaves the stored
  value alone, while an absent `size` on create or an explicit empty string
  on either resets it to `m`. Any other value is rejected with 400
  `invalid_size`.
- `GET .../tasks` and `GET /api/v1/tasks` accept `?size=xs|s|m|l|xl` to
  narrow the list and `?sort=size` to order biggest-first (`xl` → `xs`),
  falling back to the default creation order to break ties — the same shape
  `?priority=`/`?sort=priority` already has.
- `GET /api/v1/tasks/{taskID}/context` reports `size`, so an agent picking up
  a task knows how large the work is expected to be before it starts.

In the web app, size is a select on the task single view's edit form
(labelled with its point weight, e.g. "L (5 pts)"), a badge on task rows and
the task single view, and a filter plus a sort option on the task collection.
The badge is deliberately neutral at every size rather than escalating in
colour the way priority does: size is not urgency, and a red XL would read as
a problem when it only means "this is big".

**Every task predating this feature reads as `m`.** That cannot be
backfilled, and it means points-based velocity is exactly 3x the task count
until sizes are actually set — see the `sizedTaskRatio` field and the note
the Velocity card shows on its Points tab.

## Task, backlog & epic assignee

A task, a backlog and an epic each carry an `assigneeUserId` — the **FlowLens
project member who owns the work**, drawn from `project_members`. It is
optional everywhere: nullable in the schema, never in a `required` request
field, and every object starts unassigned.

This is deliberately a second axis alongside a task's existing
`assigneeGitlabUserId`, which mirrors the GitLab issue's own assignee and
syncs both ways. The two are connected by a **one-way bridge**:

- Setting `assigneeUserId` also sets the GitLab assignee, when that member has
  a GitLab identity registered for the project's connection
  (`user_gitlab_identities`, see [GitLab user identity](gitlab-sync.md#gitlab-user-identity)).
  That is what puts the assignment on the issue.
- A member with no registered identity is still a perfectly good assignee —
  the task is simply assigned inside FlowLens only, and the GitLab assignee is
  cleared rather than left pointing at someone else.
- **A GitLab-side assignee change never writes back to `assigneeUserId`.** The
  FlowLens assignee records what a human decided, so an inbound sync cannot
  silently reassign the work.
- An explicit `assigneeGitlabUserId` in the same request always wins over the
  bridge, which is how assigning a GitLab user who has no FlowLens account
  keeps working.

A backlog's and an epic's assignee have no bridge at all: neither has a
GitLab counterpart, so both are app-only end to end, like `baseBranch` and
`allowedScope`.

- `POST`/`PATCH` on `/api/v1/projects/{projectID}/tasks` /
  `/api/v1/tasks/{taskID}`, `/api/v1/projects/{projectID}/backlogs` /
  `/api/v1/backlogs/{backlogID}`, `/api/v1/projects/{projectID}/epics` /
  `/api/v1/epics/{epicID}`, and `POST .../tasks/bulk` all accept
  `assigneeUserId`. It follows the same partial-update contract as the rest of
  the object: a body without the key leaves both axes alone — which is what
  stops a PATCH of some unrelated field from reassigning the GitLab issue as a
  side effect — and an explicit `null` unassigns. A user who is not a member of
  the project is rejected with 400 `invalid_assignee`.
- `GET .../tasks`, `GET /api/v1/tasks` (cross-project), `GET .../backlogs` and
  `GET .../epics` accept `?assignee=`, which takes **`me`, a user UUID, or
  `unassigned`**. Previously it took only `me`; a UUID is what lets a lead see
  what someone else is carrying. For a task the filter matches on *either*
  axis — the FlowLens assignee or that same user's GitLab identity — so a task
  synced in from GitLab and a purely local one both show up for the same
  person. `unassigned` is the complement: assigned to nobody on either axis.
  Over a bearer token, `me` resolves to the token's project owner, since a
  token acts as that owner everywhere else in the API
  ([ADR-0009](../decisions/0009-why-project-scoped-api-tokens.md)).
- `GET /api/v1/tasks/{taskID}/context` carries the assignee too, so an AI agent
  reading its context knows whether the work is already someone's, and whose.
- The web app's create/edit forms for all three objects (and each object's
  single view) carry an assignee picker (`components/AssigneeField.tsx`)
  populated from `GET /api/v1/projects/{projectID}/members` — which is why
  that listing endpoint is open to any project member rather than owner-only
  (see [Project membership](teams.md#project-membership)). A task's picker sits
  beside its separate GitLab-assignee picker, labelled distinctly so the two
  axes are never confused for one field.

`assigneeUsername`/`assigneeDisplayName` are resolved from `users` on read
rather than stored, so a rename is picked up without a backfill; both are `""`
when unassigned. Deleting a user unassigns their work rather than deleting it
(`ON DELETE SET NULL`).

The 000031 migration backfills `assignee_user_id` for tasks already assigned to
a GitLab user who is both a project member and has that identity registered —
without it every pre-existing task would read as unassigned. Skipping the
backfill loses nothing but display, since the `?assignee=` filter ORs the two
axes anyway.

## Backlog & epic close

A backlog and an epic each also carry a `status` — `open` or `closed`,
defaulting to `open` — plus the `closedAt` timestamp of when it last became
the latter. It is the same open/closed concept a task already has, one and two
rungs up.

The problem it solves is narrow: a shipped backlog had nowhere to go. Once a
feature is released, its backlog stays in the collection view forever —
`progress = done` says *the work finished*, which is a different statement from
*this is no longer something we are tracking*, and a backlog that was abandoned
rather than delivered never reaches `done` at all. Closing it takes it off the
screen without deleting it or touching anything inside it.

**It is not a task's `status`, despite the name.** A task's mirrors the GitLab
issue state and syncs both ways; a backlog's and an epic's are app-only end to
end, like `priority`, `progress`, `baseBranch` and `assigneeUserId` before
them. Neither rung has a GitLab counterpart at all — GitLab CE has no Epic, and
a backlog is not a milestone — so closing one enqueues nothing and no GitLab
event ever moves it.

### Closing does not cascade

Closing a backlog leaves its epics open. Closing either leaves their tasks
exactly as they were — same `status`, same `progress`, still workable, still
counted by [velocity](metrics.md#velocity-issue-195) and the forecast.

That is deliberate, and it is the whole reason the feature is shaped this way.
A cascade would have to do one of two things, and both are wrong:

- **Close the tasks.** A task's close writes `closed_at` and enqueues an
  `issue.close` for GitLab. `internal/velocity` reads `closed_at` as a
  completion signal (`min(closed_at, first progress='done' transition)`), so
  closing 50 leftover tasks at once would post a 50-task completion spike on
  the day the backlog was retired — inventing throughput that never happened —
  while closing 50 GitLab issues nobody asked to close, asynchronously through
  the outbox, with no way to tell afterwards which of them were already closed
  and therefore no correct `reopen`.
- **Move the tasks to `progress = done`.** That says unfinished work finished,
  and lands in the same velocity series by the other door.

Leftover open work is **moved to another backlog** — the ordinary
`PATCH /api/v1/tasks/{taskID}` with a new `backlogId`, which never re-targets
an issue that already exists — or simply left where it is. Closing a task is
still a per-task decision, made on the task.

### API

- `POST /api/v1/backlogs/{backlogID}/close` and `.../reopen`, and
  `POST /api/v1/epics/{epicID}/close` and `.../reopen`. Both are on the
  bearer-token allowlist and need `write` scope, like a task's own
  `/close`. Closing an already-closed object is a no-op that leaves `closedAt`
  where it is, so a re-close never moves the timestamp; the same holds for
  reopening an already-open one.
- `status` is **not** writable through `PATCH` on either object — a close is
  an event, not a field edit, exactly as it is for a task.
- `GET /api/v1/projects/{projectID}/backlogs` and `.../epics` accept
  `?status=open|closed|all`. **An omitted `?status=` is not "no filter" here:**
  it means `open`, so a closed object leaves the collection without anyone
  asking. That default is the point of the feature. `?status=all` is how a
  caller gets both — needed wherever the result is a lookup table (resolving a
  task's `backlogId` to a name) rather than a browsable list.
- `GET` on the object itself always returns it whatever its status, so a
  bookmark or a task's `backlogId` never dead-ends.
- `GET /api/v1/tasks/{taskID}`'s embedded `epic` object carries the epic's
  `status` too — since the close doesn't cascade, the task's own status would
  otherwise never reveal that the rung above it has been retired.

### In the web app

Both single views carry a **Close backlog** / **Close epic** button beside
Edit and ahead of Delete, and show a "Closed" badge next to the name. There is
no confirmation step: nothing is destroyed, nothing leaves FlowLens, and the
button reopens it again — closing costs one click to undo, unlike deleting.

Both collection views gain a **Status** filter (Open, the default / Closed /
All statuses) alongside priority and progress, in the URL like every other
filter, with `status=open` dropping out of the query string. A closed object is
marked with a "Closed" badge in list rows; an open one carries no badge, since
open is the overwhelming default and a badge on every row would say nothing.

## A backlog's base branch

A backlog can also name the branch its tasks are meant to branch from during
development (e.g. `main`, `release/2.4`) — `baseBranch` on
`POST /api/v1/projects/{projectID}/backlogs` and
`PATCH /api/v1/backlogs/{backlogID}`, editable from the same create/edit form
and shown on the Backlog single view. It is optional, validated as a git
branch name when non-empty (400 `invalid_base_branch` otherwise), app-only,
and never synced to or from GitLab — unlike a merge request's own base
branch, which mirrors an actual GitLab merge request. It is also surfaced on
`GET /api/v1/tasks/{taskID}/context` (resolved through the task's backlog, or
`""` if unfiled or unset), so an AI agent working a task knows what branch to
start from.

## A backlog's allowed/forbidden change scope

A backlog can also name the paths its tasks may and may not touch —
`allowedScope`/`forbiddenScope` on the same create/edit endpoints and form,
and shown on the Backlog single view. These used to be per-task fields on
`task_ai_contexts`, but in practice they describe a sub-area of the
codebase rather than one unit of work, so they moved to the backlog: set
once, they apply to every task filed in it. Optional, capped at 20000
characters (400 `invalid_scope` otherwise), app-only, and never synced to
GitLab. Like `baseBranch`, they are surfaced on
`GET /api/v1/tasks/{taskID}/context` resolved through the task's backlog
(`""` if unfiled or unset) — a task's own `acceptanceCriteria`/`aiContext`
(set via `PUT /api/v1/tasks/{taskID}/ai-context`) remain the place for
anything task-specific.

## Bulk task creation

A spec-driven breakdown of a backlog typically produces 10-30 tasks and the
dependencies between them at once (issue #201). Creating those one `POST
/tasks` at a time is unsafe for that: each call creates a GitLab issue in the
same transaction as the task write (see "GitLab CE connection & sync"
above), so a failure partway through a manual loop leaves a half-decomposed
backlog with no way to unwind the issues already created, and dependencies
can't be wired up at all until every task's ID exists.

`POST /api/v1/projects/{projectID}/tasks/bulk` creates a whole batch of tasks
and the dependencies between them in one request and one transaction:
either every task, every dependency and every `issue.create` outbox job
commits, or none of it does. Requires write scope for a bearer token, the
same as `POST /tasks`.

- Request body: `{tasks: [...], dependencies: [...]}`. Each task carries a
  `ref` — a temporary ID valid only within that request, since the tasks
  don't have real IDs yet — plus the same fields `POST /tasks` accepts
  (`title` required; `description`, `backlogId`, `labels`, `dueOn`,
  `startDate`, `priority`, `size`) and an optional inline `aiContext`
  (`acceptanceCriteria`, `aiContext`, upserted alongside the task). Up to
  100 tasks per request.
- Each dependency is `{predecessorRef, successorRef}`, naming two `ref`s
  from the same request's `tasks` — a bulk dependency can only connect two
  tasks created in the same batch, not an existing task by ID. The
  single-dependency endpoint (`POST /task-dependencies`) already covers
  existing-to-existing edges; wiring together the batch just created is
  bulk's whole purpose, so that's the only case it supports.
- Every task and dependency is validated before anything is written — an
  empty or duplicate `ref`, an invalid field, a `ref` a dependency doesn't
  recognize, a self-dependency, or a dependency that would create a cycle
  (checked against the new batch's own edges, the same reachability check
  `POST /task-dependencies` uses) all fail the whole request with 400 and
  name the offending `ref` in the error message. Nothing is written on any
  failure.
- Response: `{tasks: [{ref, task}], dependencies: [...]}`, so a caller can
  resolve its own `ref`s to the tasks' real IDs.

