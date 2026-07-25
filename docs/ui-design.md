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
| `Organization` | A GitLab group the team works in | `organizations` |
| `Project` | A repository whose delivery flow we measure | `repositories` |
| `MergeRequest` | One change under review | `pull_requests` |
| `Reviewer` | A person assigned to review a merge request | `pull_request_reviewers` |
| `SyncRun` | One attempt to pull fresh data from GitLab | `sync_runs` |
| `User` | An account of this app | `users` |

Two notes on this table:

- Most of these are unpopulated until later phases (see `CLAUDE.md`). The model
  is the target, not a description of what ships today.
- The tables carry GitHub-era names (`repositories`, `pull_requests`,
  `github_*`) while the domain is GitLab. **The UI uses the GitLab vocabulary**
  — Project, Merge Request — and rule 1 below applies to it.

Objects that don't exist yet (`Pipeline`, `Release`) get added to this table
when they get a table, not before.

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
