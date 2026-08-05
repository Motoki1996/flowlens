# Plan: closing the API/UI gaps under `/projects/[projectId]`

**Status: all three phases shipped (2026-08-05) — delete this plan once the
branch merges, per [`docs/plans/README.md`](README.md). What survives it is
already in [`README.md`](../../README.md).**

This plan is time-limited (see [`docs/plans/README.md`](README.md)). Delete it
once every phase below has shipped, folding what survives into
[`README.md`](../../README.md) and [`docs/ui-design.md`](../ui-design.md).

## What was audited

Every endpoint called by the web screens under
`apps/web/app/projects/[projectId]` — the project single view, `tasks`,
`tasks/[taskId]`, `backlogs`, `backlogs/[backlogId]`, `gitlab-connection`,
`linked-gitlab-projects/[linkId]` and the shared `layout.tsx` — was matched
against `apps/api/internal/http/server.go` and its handlers.

**No endpoint is missing.** All 41 (method, path) pairs the UI calls resolve on
the real chi router; every request-body field the UI sends exists on the
corresponding Go request struct; `available-projects?search=` and
`webhook-events?per_page=` are both parsed; and `apps/web/types/index.ts`
matches the Go DTOs' `json` tags field for field.

What follows is the other direction: capabilities the API already has that no
screen reaches, plus one endpoint worth adding.

## Gaps

| # | Gap | Impact |
| --- | --- | --- |
| 1 | `PATCH /api/v1/linked-gitlab-projects/{linkID}` (syncScope / syncLabels / isDefault) is never called | Only the *first* link is default (set by `CreateLinkedGitlabProject`'s SQL). With two or more links there is no way to change it, and a task's assignee/label pickers read from the default link only — so they can be stuck pointing at the wrong GitLab project |
| 2 | `DELETE /api/v1/projects/{projectID}/gitlab-connection` is never called | The connection screen offers Test and Reconnect but no Disconnect |
| 3 | `DELETE /api/v1/tasks/{taskID}` is never called | A task can be closed but never deleted from the UI |
| 4 | `GET /api/v1/linked-gitlab-projects/{linkID}` does not exist | The link's single view loads the whole collection and `find()`s its own record — contrary to the OOUI rule that a single view fetches its own object ([`docs/ui-design.md`](../ui-design.md)) |
| 5 | `GET .../webhook-events/{eventID}` (payload) and `?status=` / `?page=` are never called | The event list is a fixed 10 rows, with no paging and no way to see a delivery's payload |
| 6 | `available-projects`' `nextPage` is ignored | GitLab project search shows only the first page of results |
| 7 | Project-scoped list filters/sorts are unused, and asymmetric | The Task list filters and sorts entirely client-side. Worse, `GET /projects/{id}/tasks?sort=` accepts **only** `priority`, while the UI's sort menu (and the cross-project `GET /tasks`) also offer `dueOn` and `updatedAt`. Neither endpoint has a `q` text search |

## Phases

Run `make test` and `make lint` after each phase and report the real output.

### Phase 1 — the functional holes (web only, gaps 1–3) — **done**

1. `apps/web/components/LinkedGitlabProjectDetail.tsx`: add a "Set as default"
   action and an edit form for sync scope / labels, both calling
   `PATCH /api/v1/linked-gitlab-projects/{linkId}`, then `router.refresh()`.
   Hide "Set as default" when the link already is one.
2. `apps/web/components/GitlabConnectionDetail.tsx`: add a Disconnect action
   calling `DELETE /api/v1/projects/{projectId}/gitlab-connection`, behind the
   same inline confirmation `UnlinkButton` already uses. Say in the
   confirmation that unlinking removes the linked projects with it.
3. `apps/web/components/TaskDetail.tsx`: add a destructive Delete action
   calling `DELETE /api/v1/tasks/{taskId}`, again inline-confirmed, navigating
   to `tasksPath(projectId)` on success.

Tests: one case per branch (action succeeds / API error surfaces /
confirmation cancels) per [`docs/testing.md`](../testing.md), in
`LinkedGitlabProjectDetail.test.tsx`, `TaskDetail.test.tsx` and a new
`GitlabConnectionDetail.test.tsx` (the connection component had no test file
of its own; the page-level `gitlab-connection.test.tsx` covers rendering only).

Shipped as described. Two test fixtures that were already failing on `main`
before this work — `tasks.test.tsx`'s partial task objects (no `labels`) and
`task.test.tsx`'s `@/lib/api` mock missing issue #80's
`getLinkedGitlabProjects`/`Members`/`Labels` — were repaired in the same pass,
so `make test` is green again (271 tests).

### Phase 2 — single-link endpoint (gap 4) — **done**

4. Added `GET /api/v1/projects/{projectID}/linked-gitlab-projects/{linkID}`
   with `handleGetLinkedGitlabProject` and `linkedproject.Service.Get`, which
   reuses the existing owner-scoped `GetLinkedGitlabProjectForOwner` query and
   the `GetLinkedGitlabProjectProjectID` lookup — no new SQL, no migration, no
   `sqlc` regeneration.
5. Added `getLinkedGitlabProject(projectId, linkId)` to `apps/web/lib/api.ts`
   and replaced the list-and-`find()` in
   `app/projects/[projectId]/linked-gitlab-projects/[linkId]/page.tsx`.

**Deviation from the plan as written:** the route is project-nested rather
than the flat `/linked-gitlab-projects/{linkID}` the plan proposed. A link's
response carries no project of its own — unlike a task or a backlog, a linked
GitLab project has no `project_id` column, only a path through
`gitlab_connections` — so a flat read gives the page nothing to check the URL's
project against, and the plan's "keep the project-scoping check" is
unimplementable without widening the DTO (which would touch every read path
and the `FakeQuerier`). Nesting moves the check server-side instead: another
project's link is a 404, which is also what the task and backlog single views
end up showing. The mutations stay flat, as they were.

Tests: `TestHandleGetLinkedGitlabProject_ReturnsTheLink` plus a table-driven
`_ForeignLinkGets404` covering another user's link, another of the caller's own
projects, and an unknown ID. `loginSessionAs` was extracted from
`loginSession` so a second session is one line.

### Phase 3 — list and pagination polish (gaps 5–7) — **done**

6. `parseTaskListFilter` now accepts `sort=dueOn` and `sort=updatedAt`
   alongside `priority`, so the project-scoped list takes exactly the values
   `GET /api/v1/tasks` does and rejects the rest with 400.

   **Deviation:** the two new orders are applied in `task.Service.List`
   (a stable sort keeping the manual position order as tiebreak), not in
   `ListTasksByProject`'s `ORDER BY`. Adding them to the query means changing
   its parameters, and `make generate` with the currently installed sqlc
   (v1.31.1) rewrites *every* generated file — most importantly it emits
   per-query row types (`ListTasksByProjectRow`) where the committed code
   returns the shared `Task` model, which would cascade through
   `internal/task`, `internal/webhookapply` and `dbtest.FakeQuerier`. The
   checked-in generated code was produced by an older sqlc; **upgrading it is
   its own change and its own PR**, and this feature was not the place to
   force it. Ordering in the service is exact here because the project-scoped
   list has no `LIMIT` — no ordering can change which tasks come back, only
   their sequence. If the sqlc upgrade lands, moving these two into SQL is a
   contained follow-up.
7. Webhook events: **Show more** pages through `?page=`, and each row's
   **View payload** fetches `GET .../webhook-events/{eventID}` on first open.
   Paged rows are held next to the server-rendered first page so a
   `router.refresh()` (after a retry) still replaces the list cleanly.
8. `LinkedGitlabProjectListSection` now follows `nextPage` in the
   available-projects search, appending pages rather than replacing them.
9. **Deliberately deferred:** moving the Task/Backlog list filtering and
   sorting server-side. The current client-side implementation is URL-synced
   and functionally equivalent at today's volumes; revisit when a project's
   task count makes the full-collection fetch the bottleneck. A `q` search
   parameter on either list endpoint is out of scope here too.

Tests: `TestService_List_SortsByDueDateAndRecency` (domain, both new orders)
and a table-driven `TestHandleListTasks_SortQueryAcceptsTheCrossProjectValues`
(HTTP, the wire contract only); `WebhookEventSection.test.tsx` gains paging
and payload cases; `LinkedGitlabProjectListSection.test.tsx` is new and covers
search paging.
