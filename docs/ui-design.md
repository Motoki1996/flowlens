# UI design guide — OOUI

> **Status: adopted.** These are the rules for every new web screen in
> `apps/web`. Existing screens predate them and are migrated opportunistically,
> not in a big bang.

How we decide **what screens exist and how they relate** in FlowLens. This
document is about UI *structure*; [`storybook.md`](./storybook.md) is about
covering the *states* of that structure, and [`testing.md`](./testing.md) about
verifying *behaviour*.

## The principle: objects first, tasks second

FlowLens is an exploration tool — a user arrives with a question ("why is this
project's review step slow?"), not with a task to execute. Task-oriented UI
answers that badly: every new question adds a new "do X" screen, and the screen
list grows one entry per feature.

So we design **object-oriented UI (OOUI)**: identify the objects in the domain
first, give each one a place in the UI, and let actions hang off those objects.

The design order is always:

1. **Extract objects** — the nouns the user thinks in.
2. **Design views and navigation** — collection view and single view per object,
   and the links between them.
3. **Design presentation** — layout, components, wording.

Never start at step 3. A screen that can't be explained as "the *collection* of
X" or "a single X" is a signal that step 1 was skipped.

## The object model

The objects the UI is built from, and where they live today:

| Object | Meaning | Backing table |
| --- | --- | --- |
| `User` | An account of this app | `users` |
| `Project` | A workspace owned by one user: backlogs, tasks, one GitLab connection | `projects` |
| `Backlog` | An app-only grouping of tasks inside a project | `backlogs` |
| `Epic` | An optional, app-only grouping between a `Backlog` and its tasks — the coarse unit (one screen, one endpoint group) a backlog is cut into | `epics` |
| `Task` | One unit of work, optionally mirrored by a GitLab issue | `tasks` (+ `task_ai_contexts`, `task_gitlab_links`) |
| `GitLabConnection` | A GitLab CE base URL and access token for one project | `gitlab_connections` |
| `LinkedGitLabProject` | A GitLab project a `Project` syncs issues with | `linked_gitlab_projects` |
| `SyncRun` | One import / re-sync attempt against a linked GitLab project | `gitlab_sync_runs` |
| `WebhookEvent` | One received GitLab webhook delivery and its processing state | `webhook_events` |
| `Repository` | The merge-request-tracking sibling of a `LinkedGitLabProject` | `repositories` |
| `MergeRequest` | One change under review | `merge_requests` |
| `Reviewer` | A person assigned to review a merge request | `merge_request_reviewers` |

Two notes on this table:

- The first eight rows (issue sync / task tracker) are implemented and populated
  today. `Repository` and `MergeRequest` are also populated, by the read-only
  merge-request sync in `internal/mrsync` (issue #111); `MergeRequest` also has
  its own collection/single views (issue #112, see the screen map below).
  `Repository` deliberately has no view of its own (rule 3's escape hatch,
  below) and `Reviewer` stays unpopulated — `merge_request_reviewers` exists in
  the schema but nothing writes to it yet (see `CLAUDE.md` and
  [ADR-0011](decisions/0011-why-merge-request-sync.md)).
- `Repository` and `LinkedGitLabProject` are both "a GitLab project" but name
  different things: `Repository` is the merge-request feature's
  issue-sync feature's object, backed by `linked_gitlab_projects`. `Project`
  used to mean `Repository` before the issue-sync MVP claimed the noun for the
  app-level workspace — see [ADR-0008](decisions/0008-why-per-project-gitlab-connection.md)
  for why the rename happened and why the two GitLab-project objects don't share
  a name. There is no separate `Organization` object: merge-request sync reuses
  the per-project `GitLabConnection` (ADR-0008), so a `Repository` hangs off its
  `LinkedGitLabProject` directly rather than off a GitLab group — see
  [ADR-0011](decisions/0011-why-merge-request-sync.md).

Objects that don't exist yet (`Pipeline`, `Release`) get added to this table
when they get a table, not before.

## The screen map

The routes that exist today, and the object each one is about:

| Route | Object | View |
| --- | --- | --- |
| `/projects` | `Project` | Collection |
| `/projects/[projectId]` | `Project` | Single |
| `/projects/[projectId]/backlogs` | `Backlog` | Collection (Board / List / Timeline view modes; Board is the default, its axis progress) |
| `/projects/[projectId]/backlogs/[backlogId]` | `Backlog` | Single (editing is inline here — no `/edit` route, per rule 4; the collection view's List rows share the same form) |
| `/projects/[projectId]/epics` | `Epic` | Collection (Board / List / Timeline view modes, exactly as `Backlog`; `?backlog=`/`?priority=`/`?progress=`/`?sort=` filters) |
| `/projects/[projectId]/epics/[epicId]` | `Epic` | Single (editing is inline here — no `/edit` route, per rule 4; the collection view's List rows share the same form) |
| `/projects/[projectId]/tasks` | `Task` | Collection (Board / List / Timeline view modes; Board is the default, its axis progress; `?backlog=`/`?progress=` filters) |
| `/projects/[projectId]/tasks/[taskId]` | `Task` | Single (editing is inline here — no `/edit` route, per rule 4) |
| `/tasks` | `Task` | Collection, cross-project (`?status=`/`?priority=`/`?progress=`/`?sort=`/`?projectId=` filters) |
| `/projects/[projectId]/merge-requests` | `MergeRequest` | Collection (`?state=`/`?author=`/`?taskId=`/`?since=`/`?until=`/`?sort=` filters; read-only, no view modes — see rule 5) |
| `/projects/[projectId]/merge-requests/[mrId]` | `MergeRequest` | Single (read-only: review/pipeline status, and a link to its linked `Task` if any) |
| `/projects/[projectId]/gitlab-connection` | `GitLabConnection` | Single (+ the `LinkedGitLabProject` collection) |
| `/projects/[projectId]/linked-gitlab-projects/[linkId]` | `LinkedGitLabProject` | Single (+ its `SyncRun` and `WebhookEvent` history) |
| `/dashboard` | — | Aggregation of teasers onto `Task` and `Project` (see below) |
| `/login`, `/signup` | — | Auth flows (rule 10) |

Backlogs and tasks exist only inside a project, so both halves of each pair are
nested under it. The flat `/backlogs/[id]` and `/tasks/[id]` routes predate the
nesting and now only redirect to their nested equivalents, so older links keep
working. Route strings are built by `lib/routes.ts` rather than written inline.

`/tasks` is the one deliberate exception to that nesting: the same `Task`
object, but queried across every project a user owns instead of scoped to
one, so "what should I be doing right now" doesn't mean opening each project
in turn. It has no single view of its own — each row still links to its
`Task`'s one canonical single view, `/projects/[projectId]/tasks/[taskId]`.

The project single view is a **hub**: identity and attributes, then a link per
related collection with a count, not the collections themselves. A screen that
starts accumulating other objects' lists is the signal to split it — that is
what happened to this one.

Every screen under `/projects/[projectId]` shares one layout
(`app/projects/[projectId]/layout.tsx`), which holds the app header and a
**persistent project sidebar**: a switcher for the project itself, then one
entry per section (Overview, Backlogs, Epics, Tasks, Merge requests, GitLab connection) with the same
count the hub shows. The hub being the only way between sibling collections was
the original mistake — going from Backlogs to Tasks meant a detour up and back
down. The sidebar makes that lateral move one click, and the sections are the
`ProjectSection` union in `lib/routes.ts`, so the switcher can keep you on the
same section when you change project.

Because the sidebar always names the project, breadcrumbs inside it start at the
collection (`Backlogs / Sprint 1`), and the collection views drop them
altogether — their own entry in the sidebar is already marked current.

The sidebar is shadcn's `Sidebar` (`components/ui/sidebar.tsx`), so it
**collapses** to an icon rail — from the toggle in the header, or ⌘B — and
becomes a drawer below the mobile breakpoint. Its **width is draggable** by
the handle on its right edge (`components/SidebarResizer.tsx`, ours: shadcn
has no such handle, it only reads a `--sidebar-width` variable), which is also
adjustable from the keyboard and resets on double-click. Both the open/closed
state and the width persist in cookies and are read by the layout on the
server (`lib/sidebar.ts`), never restored on the client afterwards — a screen
must not paint at one width and then jump to another. A screen that is not
under a project has no sidebar and therefore no toggle: `AppHeader`'s
`leading` slot is empty there.

Which means the sidebar is furniture, not content: an entry may be reduced to
its icon at any time, so every section needs an icon that identifies it alone,
and nothing may live *only* in the sidebar.

`/dashboard`, the screen every login lands on, is the other deliberate
exception: it is not a view of one object, but read-only teasers onto two —
overdue/due-soon/waiting-to-start/high-priority slices of the `Task`
collection and a sync-failures/recently-updated slice of the `Project`
collection, each capped and linking out to the full collection view it's
filtered from (rule 5). It carries no edit actions of its own (rule 4): a
sync failure links to that `Project`'s single view rather than growing a
retry button here, the same as every other section defers to the single view
it teases.

Four objects deliberately skip routes of their own (rule 3's escape hatch):

- `GitLabConnection` has a single view but **no collection view** — a project
  has at most one connection ([ADR-0008](decisions/0008-why-per-project-gitlab-connection.md)),
  so a list of one would be noise. The project view links straight to it.
- `SyncRun` and `WebhookEvent` are never browsed apart from the link that
  produced them, so they appear only as related collections inside the
  `LinkedGitLabProject` single view.
- `Repository` has no view of its own either — like `GitLabConnection`, a
  project has at most one per `LinkedGitLabProject`, and it carries nothing a
  user would browse independently of the `MergeRequest`s it groups. `Reviewer`
  would join this list once populated (see the object model note above), the
  same "nested only" reasoning `SyncRun`/`WebhookEvent` already follow.

## Rules

### 1. One object, one name, everywhere

An object has a single name shared by the UI label, the route, the component,
the API resource, and — where it isn't already fixed — the table. If a rename is
too expensive to do everywhere, the UI still uses one name consistently and the
mapping is recorded in the table above. Never let two names for one object be
visible to the user.

### 2. Screens are named with nouns, not verbs

`/projects`, not `/manage-projects`. `/merge-requests/[id]`, not
`/inspect-merge-request`. If a screen name needs a verb, it is probably an
action that belongs inside an object's view (rule 4).

### 3. Every object gets a collection view and a single view

The two-view pair is the default shape, and routes mirror it:

- Collection: `/merge-requests` — many objects, one row/card each.
- Single: `/merge-requests/[id]` — one object in full.

A collection view links to its single view; a single view links back and to
related objects. If an object only ever appears nested inside another (e.g.
`Reviewer` inside a merge request), it may skip its own routes — but say so
deliberately rather than by omission.

### 4. Actions live on the object they act on

"Sync now", "Deactivate project", "Retry" are verbs attached to a `Project` or a
`SyncRun`, placed in that object's collection or single view. Do not build a
standalone task screen (a "Sync" page) to host them.

An action that operates on many objects at once belongs in the collection view,
driven by row selection.

### 5. A collection is one dataset, presented several ways

Table, card grid, and chart of the same collection are **view modes of one
screen**, not separate screens. Filters and sorts are derived from the object's
own attributes (state, author, project, merged-at), so the user filters in the
same vocabulary the object is described in.

A filter that another screen wants to hand off through belongs in the URL. The
Task collection reads `?backlog=` and `?epic=`, and the Backlog and Epic
screens link to `/projects/[id]/tasks?backlog=[backlogId]` (or `?epic=`)
instead of growing task browsing of their own — one place to browse tasks, reachable pre-filtered. The Backlog
single view keeps read-only previews of both its children — epics and tasks —
in **one card with an Epics / Tasks tab switch**, each tab carrying its own
count and its own "Open in Epics" / "Open in Tasks" handoff. Two stacked lists
would have doubled the screen's height to show two things only one of which is
being read at a time; the tab labels also stand in for a card title, since
each names the object it shows. Filtering, the board and timeline modes, and
task creation all stay with the collection that owns them.

**Creation is the deliberate exception to that handoff.** Both tabs carry a
"New epic" / "New task" action that opens the owning collection's own form
inline, with this backlog pre-filled. An epic and a task are each created
*into* a backlog: the collections' forms have to ask which one, and this
screen already knows — so making the user go there to answer a question the
context already answers is the worse trade. The forms themselves are shared
(`EpicForm`, `NewTaskForm`), so the two screens can't drift on what a field
is.

An inline "New …" form is separated from the list below it by a rule and
generous spacing. A form and a list of rows are both stacks of bordered
boxes; without the separation it is genuinely unclear where the form ends.

A relationship between two objects is editable from both ends, in the
vocabulary of whichever object you are looking at. A task names its epic
(the Epic control on the Task single view, and the Epic field when creating
one); an epic names its tasks (a tickable list of its backlog's free tasks, on
the epic's own form and single view). Neither is "the" place — which one you
reach for depends on whether you are thinking about one task or about the
shape of a coarse unit.

A list you pick several things out of has to survive a backlog with hundreds
of tasks, so the epic's task picker is built on the `Command` primitive
(cmdk): arrow keys move, Enter ticks, and a search box narrows as you type.
It also hides closed tasks by default and offers a "selected only" view.

Two rules hold there, and hold anywhere a filter meets a multi-selection:
**a filter changes what is shown, never what is selected** — the set is saved
whole, so a row hidden by a search is still in it — and **a bulk action reaches
only the rows currently visible**, stating its own count ("Select all (12)").
A "clear" that also dropped hidden selections would unfile work the reader
never saw.

`Backlog` and `Epic` share their Board and Timeline view modes
(`GroupBoardSection` / `GroupTimelineSection`, `lib/groups.ts`): an epic is
deliberately shaped as a backlog that lives inside a backlog, and a board that
only needs a name, a progress and a task ratio has nothing to tell the two
apart. Each object still names its own component, so a screen reads as being
about the object it is about.

### 6. Single views present attributes in a fixed order

Identity (title, number, state) → attributes (branches, size, timestamps) →
related collections (reviewers, sync history). Keeping the order stable across
objects is what makes a new screen feel already-learned.

### 7. User-authored long text is Markdown

A task's or backlog's description, a project's description and a task comment
are rendered as GitHub-flavoured Markdown by `components/Markdown.tsx` — never
as raw text and never with `dangerouslySetInnerHTML`. A description
round-trips with a GitLab issue's own description, which is already Markdown,
so this is the rendering half of a format the data has always been in; the
same component's autolinking is what makes a pasted URL clickable. Every
`<textarea>` that edits one of those fields says so beneath it, and any new
long-text field should use the same component rather than
`whitespace-pre-wrap`. Short single-line attributes (a name, a branch, a
label) stay plain text.

### 8. An enumerated value reads the same everywhere, in two forms

`priority`, `progress`, `status`, `size` — every small closed set — has one
label and one accent colour, owned in `lib/` (`PRIORITY_LABELS`/
`PRIORITY_ACCENT`, `PROGRESS_LABELS`/`PROGRESS_ACCENT`) and rendered by one
component pair, never re-spelled inline in a screen. It appears in exactly two
forms:

- **a badge** (`PriorityBadge`, `ProgressBadge`) where the value is being
  *read* — a list row, a card, a single view;
- **a colour dot beside its label** (`PriorityDot`, `ProgressDot`) where the
  value is being *picked* — a `Select` in a create/edit form, a filter menu. A
  badge in every row of a dropdown competes with the field it sits inside. The
  dot is `aria-hidden` and the label always renders beside it, so colour is the
  scan aid and never the only way to read the value.

Order is the one thing the two forms don't share, and deliberately so. A
**board's columns** ramp left to right in the direction the value grows —
`PRIORITY_COLUMNS` is Low → Urgent, `PROGRESS_COLUMNS` is Not started → Done.
A **menu** is a ranked list scanned top-down for the value you reached for, so
priority inverts: `PRIORITY_OPTIONS` is Urgent → Low, matching every tracker a
reader arrives from. Progress keeps one order in both, because "the order work
advances" is already the order you pick in. Sizes stay uncoloured
(`SizeBadge`): size is not urgency, and a red XL would read as a problem when
it only means "this is big".

### 9. A name never grows the layout — it clips, and hover gives it back

An object's name is written by a person and can be any length, so no row or
card may be sized by it. Every collection view renders a name through
`TruncatedName`: one line in a list row, where the name shares the line with
badges and dates, two in a board card, which has the height to spare and only
its column's width to work with. The timeline's name column is the same
component, widened by its own splitter.

The full text comes back on hover or keyboard focus, and **only when the name
was actually clipped** — measured at the moment of hover, since a column can be
resized under it. A tooltip repeating a name already fully on screen is noise.

Because hover doesn't exist on touch, the tooltip is a convenience, never the
only route to the whole name: the object's single view always states it in
full, and every clipped name links there.

### 10. Authentication is the deliberate exception

Login, signup, and logout are genuinely task-shaped: one flow, one outcome, no
object to browse. They stay task-oriented, and the current `/login` and
`/signup` screens are correct as they are. This exemption covers auth only —
don't extend it to "setup" or "sync" flows.

## Checklist for a new screen

Before writing components:

- [ ] Which object is this screen about? Name it.
- [ ] Is it the collection view or the single view? (If neither — why?)
- [ ] Is the object in the table above? If not, add it.
- [ ] Does the route use the object's noun?
- [ ] Which actions belong here, and which object do they act on?
- [ ] Which related objects does it link to, and does the link go both ways?

Then continue with [`storybook.md`](./storybook.md) for the states that screen
must cover.

## Relationship to the other docs

They partition, they don't overlap:

| Concern | Where |
| --- | --- |
| Which screens exist, what they're about, how they link | This document |
| Which states of a screen are pinned down and reviewable | [`storybook.md`](./storybook.md) |
| Business rules, authz, component behaviour | [`testing.md`](./testing.md) |

The rationale for choosing OOUI is recorded in
[ADR-0006](./decisions/0006-why-ooui.md).
