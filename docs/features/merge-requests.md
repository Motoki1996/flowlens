# Merge requests

## Merge request sync (issue #111)

Read-only sync of a linked GitLab project's merge requests and their latest
pipeline status, building on [ADR-0011](../decisions/0011-why-merge-request-sync.md)'s
schema/design. **FlowLens never writes a merge request back to GitLab** —
unlike issue sync, this is one-way.

- A `repositories` row (the MR-tracking sibling of a `linked_gitlab_projects`
  row) and an initial import are both created automatically the moment a
  GitLab project is linked, alongside issue sync's own initial import — no
  separate "enable MR sync" step.
- **Webhook-primary:** linking a project's webhook now also requests
  `merge_requests`/`pipeline` events (previously issues/notes only).
  `Merge Request Hook` deliveries create/update a `merge_requests` row;
  `Pipeline Hook` deliveries update the merge request's
  `pipeline_status`/`pipeline_id`/`pipeline_updated_at` when the pipeline
  names a merge request (`merge_request.iid`) already imported — a plain
  branch/tag pipeline, or one for an MR not yet imported, is skipped.
  Idempotency is the same `gitlab_merge_request_id` UNIQUE constraint /
  strict `updated_at` staleness guard issue sync uses.
- **Periodic catch-up:** `internal/mrsync` walks every page of a
  repository's merge requests (`mr.import` sync job, the same outbox/worker
  shape as `project.import`), fetching each one's current pipeline
  (`head_pipeline`) and, once, its first review activity (earliest note from
  someone other than the author, `first_reviewed_at` — GitLab CE's
  approvals endpoint carries no per-approval timestamp, so notes are the
  only source with one) and recording the run on `repository_sync_runs`.
- **Task linking:** a merge request whose description contains a closing
  keyword (`Closes #12`, `fixes #12`, ...) or whose source branch starts
  with an issue number (`12-fix-thing`, `issue-12`) is linked to that
  issue's task via the existing `task_gitlab_links` table, giving a
  task → MR → pipeline chain. A merge request that references nothing
  recognizable is simply left unlinked.
- See [Merge request views](#merge-request-views-issue-112) for the API/UI
  that surfaces this data.

## Merge request views (issue #112)

The `MergeRequest` collection/single views the object model in
[`docs/ui-design.md`](../ui-design.md) has anticipated since before either
existed. Read-only throughout — FlowLens never writes a merge request back to
GitLab (ADR-0011), so unlike the `Task` screens there is no create/edit/delete
here.

- `GET /api/v1/projects/{projectID}/merge-requests` lists a project's merge
  requests, scoped through `repositories` → `linked_gitlab_projects` →
  `gitlab_connections` to the caller's project membership, the same
  `project_members` check the task collection uses. Filters: `?state=`
  (`opened`/`merged`/`closed`/`locked`), `?author=` (GitLab username),
  `?taskId=` (only the merge request(s) linked to one task), `?since=`/
  `?until=` (`YYYY-MM-DD`, bounding `gitlab_created_at`), `?sort=updated`
  (ranks by `gitlab_updated_at` instead of the default `gitlab_created_at`,
  both descending).
- That list is **paged**: `?page=` (1-based) and `?per_page=` (default 30,
  clamped to 100), and the response is the envelope
  `{ "mergeRequests": [...], "nextPage": 0, "totalCount": 0 }` rather than a
  bare array — `nextPage` is `0` when no further page follows, the same shape
  `GET .../webhook-events` returns, and `totalCount` is how many merge
  requests match the filter across every page. A repository synced for a year
  holds thousands of merged merge requests, and this endpoint used to return
  every one of them in a single response.
- `GET /api/v1/merge-requests/{mergeRequestID}` returns a single merge
  request, scoped the same way.
- Web: `/projects/[projectId]/merge-requests` (collection) and
  `/projects/[projectId]/merge-requests/[mrId]` (single, showing review/
  pipeline status and a link to its linked `Task` if any) — see the screen
  map in [`docs/ui-design.md`](../ui-design.md). The Task single view also
  shows a "Merge requests" card, the reverse link, via the same list endpoint
  filtered by `?taskId=`.
- The collection view opens on **open merge requests, most recently updated
  first** (`?state=` defaults to `opened`, `?sort=` to `updated`) rather than
  on the project's entire merge-request history. That is what the screen is
  for: seeing what is in flight, and drilling into the project's
  [delivery metrics](metrics.md#delivery-metrics-issue-113) when a number moves. "All
  states" stays one click away, and `?state=all` is the explicit opt-out.
  Paging is held in the URL as `?page=`, so a deep page can be linked to and
  survives a refresh; changing any filter resets to page 1.
- `?sort=` takes `created` (`gitlab_created_at` descending, the order an
  omitted `?sort=` also gives) or `updated` (`gitlab_updated_at` descending).
  `created` is deliberately a real value rather than "omit the parameter":
  the screen's own Sort select has to be able to send the default order back
  when you switch away from `updated`.
- Four indexes on `merge_requests` (migration 28) back the list's two sorts,
  each in a state-filtered and an unfiltered form. Each leads with the **sort
  key**, not `repository_id`: the project scope reaches this query through a
  join rather than a `WHERE` clause, so an index leading with `repository_id`
  is never chosen. Measured against 50k merge requests, that is 54ms → 0.16ms
  for "all states" and 15ms → 0.29ms for the view's default.

