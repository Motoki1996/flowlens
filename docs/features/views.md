# Views, search and the dashboard

## Task collection search, filters and sort

The project-scoped Task collection (`/projects/{projectId}/tasks`) narrows and
orders its List and Timeline view modes together, since both are presentations
of the same filtered set (`docs/ui-design.md` rule 5):

- A free-text box (`TaskSearchBox`, debounced 300ms and shared with the
  cross-project collection's own box below) matches a task's title or
  description.
- The status filter (All / Open / Closed) defaults to **Open**, so closed
  tasks don't fill the list; the backlog filter (unchanged) narrows further,
  and a progress filter narrows by FlowLens's own work state.
- Sort offers **Default order** (the API's own creation order, expressed by
  the absence of `?sort=`), **Due date**, **Priority**, **Progress** and
  **Recently updated**, the same four named values the cross-project Task
  collection's `?sort=` accepts (below), so the two screens agree on what
  each one means. Sorting is a display order for the request only.
- **The API applies all of it** (issue #143), not the browser: the filters
  are held in the URL (`?q=`, `?status=`, `?progress=`, `?sort=`, alongside
  the existing `?backlog=`), and changing one pushes a new query string that
  the server component turns into `GET
  /api/v1/projects/{projectID}/tasks?…` — the same round trip the
  cross-project collection makes. A reload, a shared link and the browser's
  back button all land on the same filtered list, and List, Board and
  Timeline are three presentations of that one response rather than three
  filters over a full one. Note this makes `?q=` the API's full-text match
  (below) rather than a substring match: "logi" no longer finds "login".
- There is deliberately **no pagination**: the matching tasks come back
  whole, which is what lets the List view group them by backlog.
  Capping the response is the follow-up
  to make if a project ever grows past what one response should carry.

The Backlog collection (`/projects/{projectId}/backlogs`, issue #151) carries
the same idea across its own three view modes, at a smaller scale:

- A priority filter, a progress filter and a sort (**Manual**, **Due date**,
  **Priority**, **Progress**) sit in the same `CardHeader` shape as the Task
  collection's own row. Priority and progress are held in the URL and applied
  server-side (`?priority=`, `?progress=`, `?sort=priority|progress` on `GET
  .../backlogs`, above); `?sort=dueOn` has no server-side equivalent — a
  backlog's schedule is app-only — so `BacklogListSection` sorts that case
  itself, dueOn ascending with undated backlogs last.
- The name search box (also `TaskSearchBox`, parameterized with `label`) has
  no API support at all and is matched entirely client-side: a project's
  backlogs are already fetched in full for the List/Board/Timeline views, and
  run orders of magnitude fewer than tasks, so there's nothing to gain from a
  server round trip for it. It's still held in the URL (`?q=`) for
  shareability and reload, the same as the client-only filters above.

## Paged task collections

Both task collections — the per-project `GET /api/v1/projects/{projectID}/tasks`
and the cross-project `GET /api/v1/tasks` — return **one page** of tasks, not
the whole match. A project accumulates tasks without bound, and each row
carries every column including its Markdown `description`, so an unpaged list
grew with the project until it was the slowest thing on the screen.

- `?page=` is the 1-based page number (absent means the first) and
  `?per_page=` the page size — default 50, clamped to 200 rather than
  rejected, so asking for "everything" yields a bounded page instead of a 400.
  `GET /api/v1/tasks` also still accepts `?limit=` as `?per_page=`'s original
  name.
- The response is an envelope, matching the merge-request collection's:

  ```json
  { "tasks": [ … ], "nextPage": 2, "totalCount": 137, "openCount": 96 }
  ```

  `nextPage` is `0` on the last page. `totalCount` and `openCount` (the latter
  only on the per-project list) count **the filter's whole match**, not the
  page — they are counted in SQL, which is what lets the Project single view
  show "96 open / 137 total" without fetching a single task row
  (`?per_page=1`). A filtered list reports its own totals, never the
  project's. The project sidebar's own Tasks/Merge requests badges take the
  same shortcut but show only the open count, no denominator, matching the
  Backlogs/Epics badges beside them (an omitted `?status=` there already
  means open-only) — every sidebar count is "how much is still open", not a
  fraction.
- Every `?sort=` order is applied in SQL. On an unpaged list the sort only
  decided the sequence rows arrived in, so `dueOn`/`updatedAt` were applied in
  Go; on a paged one it decides which rows a page *contains*, so all five now
  live in the query.
- **Backlogs and epics are deliberately not paged.** They run orders of
  magnitude fewer than tasks and both collection screens group by them, so
  they still return a plain array. What their lists *did* get is a cheaper
  task-count join: each backlog's/epic's `taskCount`/`closedTaskCount` now
  comes from a pre-aggregated subquery keyed by `backlog_id`/`epic_id` rather
  than a `LEFT JOIN tasks` plus an outer `GROUP BY` over every selected
  column, so the cost follows the number of backlogs rather than the number of
  tasks.
- On the web side, both collection screens carry a Previous/Next pager and
  reset `?page=` whenever a filter changes. Note the two client-side-only
  filters (`?label=` and `?due=` on the project collection, "Only with a due
  date" on the cross-project one) narrow **the page in hand**, not the whole
  match — the header count and the pager both report the server's numbers, so
  the two never masquerade as each other.

## Task full-text search

`GET /api/v1/projects/{projectID}/tasks` and the cross-project
`GET /api/v1/tasks` both also accept `?q=`, matching a task's title or
description, combinable with every other filter (`priority=`, `progress=`,
`status=`, etc.) and with `sort=`. It is backed by `tasks.search_vector`, a
`STORED` generated column (`to_tsvector('simple', title || ' ' ||
description)`) with a GIN index, so filtering happens in the database rather
than by fetching every task and matching client-side. The `'simple'` text
search configuration does no stemming or dictionary-based word segmentation —
deliberately, to avoid the extra dependency a real Japanese tokenizer
(pg_bigm/pgroonga) would add — so a query matches as long as it lines up with
a whole run of characters the parser tokenizes as one lexeme; there is no
"contains this substring anywhere" guarantee for CJK text the way there is
for space-separated words. Both Task collection screens' search boxes are
this same match: the project-scoped one stopped matching substrings
client-side when its filtering moved to the API (issue #143, above).

## Markdown descriptions

A task's and a backlog's `description`, a project's `description` and a task
comment's body are all stored as plain text and **rendered as GitHub-flavoured
Markdown** in the web app — headings, lists, task lists, tables, blockquotes,
fenced code and links. A bare URL pasted into any of them (`https://…`,
`www.…`) becomes a clickable link on its own, with no `[text](url)` needed.

This is a rendering change only: nothing about the stored value or the API
changed, and a description that contains no Markdown syntax reads exactly as
it always did. It also lines the web app up with GitLab, whose issue
descriptions — the other end of the two-way sync — have always been Markdown.

Two deliberate limits:

- **Raw HTML in a description is shown as text, not rendered.** A description
  can arrive from a GitLab issue written by anyone with access to that
  project, so the renderer (`react-markdown` + `remark-gfm`) builds a React
  tree instead of setting `innerHTML`, and raw HTML is dropped rather than
  sanitized. Links are restricted to `http`, `https` and `mailto`.
- **Images are shown as their alt text rather than fetched.** GitLab stores an
  issue's attachments as paths relative to its own project (`/uploads/…`),
  which FlowLens cannot resolve, so rendering them would reliably produce a
  broken image.

## Cross-project task collection

`GET /api/v1/tasks` returns every task across every project the authenticated
user owns — "what should I be doing right now" without opening each project
in turn, and the same underlying query both `/dashboard` (below) and the
future merge-request/CI delivery-flow dashboard build on. Unlike every other
task route, it takes no `{projectID}`: it already spans every project that
owner has.

- Query parameters, all optional and independent: `status=open|closed`,
  `priority=low|medium|high|urgent`, `dueBefore=`/`dueAfter=`/`startedBefore=`
  (`YYYY-MM-DD`, inclusive), `projectId=` (repeatable — narrows within the
  caller's own projects, never a way to reach someone else's),
  `sort=dueOn|priority|updatedAt` (default `dueOn`, ascending, tasks with no
  due date last), `assignee=me` (only tasks assigned to the caller's own
  registered GitLab identity — see [GitLab user identity](gitlab-sync.md#gitlab-user-identity); a caller with no registered identity gets an empty list, not an
  error), and `q=` (free-text over title/description — see
  [Task full-text search](#task-full-text-search) above). `page=`/`per_page=`
  page the result (default 50 per page, capped at 200) — see
  [Paged task collections](#paged-task-collections) above; `limit=` is
  `per_page=`'s original name and still works, which is what the dashboard's
  top-N teasers use. The same `assignee=me` and `q=` filters are also accepted
  on the per-project `GET .../tasks` list.
- Each task in the response carries a `projectName` field alongside every
  field `GET .../tasks/{taskID}` returns, so a cross-project list is readable
  without a second look-up per row. It never resolves GitLab sync state,
  unlike the per-project list — that would turn one query back into an N+1
  lookup; a task's own single view is still where to check that.
- **Session-only.** A project-scoped API token ([ADR-0009](../decisions/0009-why-project-scoped-api-tokens.md))
  is issued for exactly one project and has no notion of "every project I
  own", so this route is deliberately left off the bearer-token allowlist —
  see the route registration in `internal/http/server.go`.

In the web app, `/tasks` is the cross-project Task collection (see
[`docs/ui-design.md`](../ui-design.md)): the default view is open tasks
with a due date, sorted soonest-first; each row links to that task's
canonical single view under its own project. Its search box is debounced and
held in `?q=`, the same as its other filters: typing round-trips to `GET
/api/v1/tasks?q=`, exactly as the project-scoped Task collection's own box
does (see [Task collection search, filters and
sort](#task-collection-search-filters-and-sort) above). `?progress=` and
`?sort=progress` are accepted here too, deep-linkable though the screen has
no progress control of its own.

## Dashboard

`/dashboard`, the screen every login lands on, is a set of read-only teasers
built entirely from `GET /api/v1/tasks` and `GET /api/v1/projects`, not an
object of its own — it carries no edit actions, and every section links out
to the Task or Project collection it's a filtered view of
([`docs/ui-design.md`](../ui-design.md) rules 4/5):

- **Overdue** — open tasks whose `dueOn` is before today.
- **Due today / this week** — open tasks due between today and the end of
  this week. "This week" is Monday–Sunday and the boundary is computed from
  the web server's local time, the same convention `toApiDate` already uses
  for a picked calendar day; there is no other week-boundary convention in
  the codebase to match yet.
- **Waiting to start** — open tasks whose `startDate` has already arrived
  (`startedBefore=<today>`).
- **High priority** — open tasks with `priority` `urgent` or `high`, read off
  the same `GET /api/v1/tasks?sort=priority` ranking `?sort=priority` itself
  uses.
- **Assigned to me** — open tasks matching `GET /api/v1/tasks?assignee=me`
  (see [GitLab user identity](gitlab-sync.md#gitlab-user-identity)). Empty, not an
  error, for a user who hasn't registered their GitLab identity yet; the
  empty state points at `/settings`.
- **Sync failures** — projects with at least one task whose GitLab sync
  failed. `GET /api/v1/projects?failedSync=true` narrows to just those and
  populates `failedSyncTaskCount` for each — the plain (unfiltered) project
  list still always reports `0` there, same as `GET /api/v1/projects/{id}`
  is the only other place that count is populated. Each row links to that
  project's own view for the warning banner and retry.
- **Projects** — the most recently updated projects, linking to `/projects`.

A task with no due date never appears in the overdue/due-soon sections; if
the user has open tasks but none of them has a due date at all, those two
sections explain what setting one would surface instead of implying nothing
is due. A user with no projects yet sees a prompt to create one instead of
the sections.

