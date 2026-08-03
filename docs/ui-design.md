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
| `Task` | One unit of work, optionally mirrored by a GitLab issue | `tasks` (+ `task_ai_contexts`, `task_gitlab_links`) |
| `GitLabConnection` | A GitLab CE base URL and access token for one project | `gitlab_connections` |
| `LinkedGitLabProject` | A GitLab project a `Project` syncs issues with | `linked_gitlab_projects` |
| `SyncRun` | One import / re-sync attempt against a linked GitLab project | `gitlab_sync_runs` |
| `WebhookEvent` | One received GitLab webhook delivery and its processing state | `webhook_events` |
| `Organization` | A GitLab group the team works in | `organizations` |
| `Repository` | A GitLab project whose merge-request delivery flow we measure | `repositories` |
| `MergeRequest` | One change under review | `pull_requests` |
| `Reviewer` | A person assigned to review a merge request | `pull_request_reviewers` |

Two notes on this table:

- The first eight rows (issue sync / task tracker) are implemented and populated
  today. `Organization`, `Repository`, `MergeRequest`, and `Reviewer` back the
  deferred merge-request / CI delivery-flow feature; their tables exist but stay
  unpopulated until that feature ships (see `CLAUDE.md`).
- `Repository` and `LinkedGitLabProject` are both "a GitLab project" but name
  different things: `Repository` is the not-yet-built merge-request feature's
  object, still backed by the GitHub-era `repositories` table; `LinkedGitLabProject`
  is the issue-sync feature's object, backed by `linked_gitlab_projects`. `Project`
  used to mean `Repository` before the issue-sync MVP claimed the noun for the
  app-level workspace — see [ADR-0008](decisions/0008-why-per-project-gitlab-connection.md)
  for why the rename happened and why the two GitLab-project objects don't share
  a name.

Objects that don't exist yet (`Pipeline`, `Release`) get added to this table
when they get a table, not before.

## The screen map

The routes that exist today, and the object each one is about:

| Route | Object | View |
| --- | --- | --- |
| `/projects` | `Project` | Collection |
| `/projects/[projectId]` | `Project` | Single |
| `/projects/[projectId]/backlogs` | `Backlog` | Collection (List / Timeline view modes) |
| `/projects/[projectId]/backlogs/[backlogId]` | `Backlog` | Single |
| `/projects/[projectId]/tasks` | `Task` | Collection (List / Timeline view modes, `?backlog=` filter) |
| `/projects/[projectId]/tasks/[taskId]` | `Task` | Single (editing is inline here — no `/edit` route, per rule 4) |
| `/tasks` | `Task` | Collection, cross-project (`?status=`/`?priority=`/`?sort=`/`?projectId=` filters) |
| `/projects/[projectId]/gitlab-connection` | `GitLabConnection` | Single (+ the `LinkedGitLabProject` collection) |
| `/projects/[projectId]/linked-gitlab-projects/[linkId]` | `LinkedGitLabProject` | Single (+ its `SyncRun` and `WebhookEvent` history) |
| `/dashboard` | — | Aggregation of teasers onto `Task` and `Project` (see below) |
| `/login`, `/signup` | — | Auth flows (rule 7) |

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
entry per section (Overview, Backlogs, Tasks, GitLab connection) with the same
count the hub shows. The hub being the only way between sibling collections was
the original mistake — going from Backlogs to Tasks meant a detour up and back
down. The sidebar makes that lateral move one click, and the sections are the
`ProjectSection` union in `lib/routes.ts`, so the switcher can keep you on the
same section when you change project.

Because the sidebar always names the project, breadcrumbs inside it start at the
collection (`Backlogs / Sprint 1`), and the collection views drop them
altogether — their own entry in the sidebar is already marked current.

`/dashboard`, the screen every login lands on, is the other deliberate
exception: it is not a view of one object, but read-only teasers onto two —
overdue/due-soon/waiting-to-start/high-priority slices of the `Task`
collection and a sync-failures/recently-updated slice of the `Project`
collection, each capped and linking out to the full collection view it's
filtered from (rule 5). It carries no edit actions of its own (rule 4): a
sync failure links to that `Project`'s single view rather than growing a
retry button here, the same as every other section defers to the single view
it teases.

Three objects deliberately skip routes of their own (rule 3's escape hatch):

- `GitLabConnection` has a single view but **no collection view** — a project
  has at most one connection ([ADR-0008](decisions/0008-why-per-project-gitlab-connection.md)),
  so a list of one would be noise. The project view links straight to it.
- `SyncRun` and `WebhookEvent` are never browsed apart from the link that
  produced them, so they appear only as related collections inside the
  `LinkedGitLabProject` single view.

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
Task collection reads `?backlog=`, and the Backlog screens link to
`/projects/[id]/tasks?backlog=[backlogId]` instead of growing task browsing of
their own — one place to browse tasks, reachable pre-filtered. The Backlog
single view keeps a read-only preview of its tasks and an "Open in Tasks" link;
filtering, the timeline mode, and task creation stay with the collection that
owns them.

### 6. Single views present attributes in a fixed order

Identity (title, number, state) → attributes (branches, size, timestamps) →
related collections (reviewers, sync history). Keeping the order stable across
objects is what makes a new screen feel already-learned.

### 7. Authentication is the deliberate exception

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
