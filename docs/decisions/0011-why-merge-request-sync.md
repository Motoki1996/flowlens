# 0011. Why and how FlowLens will sync merge requests

- **Status:** Accepted
- **Date:** 2026-08-10

## Context

FlowLens's name promises visibility into a team's delivery process, but
nothing measures it yet. `organizations`, `repositories`, `pull_requests`,
`pull_request_reviewers` and `sync_runs` have sat empty since
[migration 000001](../../apps/api/migrations/000001_init.up.sql), reserved for
a GitHub-shaped design that predates local auth and the per-project GitLab
connection ([ADR-0008](0008-why-per-project-gitlab-connection.md)). Today
FlowLens is, in practice, a GitLab-issue mirror with a Gantt and a board —
useful, but something GitLab CE itself already gets you most of the way to.
Merge-request delivery-flow visibility is the differentiator, and this ADR is
step one of it: **design and schema only**. The sync engine itself is
[#111](https://github.com/Motoki1996/flowlens/issues/111).

Three questions have to be answered before a single column is added:

1. Where does the data come from — the existing per-project GitLab
   connection, or the per-user personal access token `CLAUDE.md` originally
   anticipated for this feature?
2. What triggers a sync, and how often?
3. What do we actually measure? Metrics come first — columns follow from
   metrics, not the other way around.

There's a fourth, schema-shaped question the reserved tables force on us:
`repositories.organization_id` requires an `organizations` row, but the only
GitLab hierarchy FlowLens actually has today is `GitLabConnection` →
`LinkedGitLabProject`, scoped to a FlowLens `Project`, with no group/org
concept anywhere in it. And the reserved tables still carry GitHub-era names
(`pull_requests`, `pull_request_reviewers`) that collide with GitLab's own
vocabulary (Merge Request), which [`docs/ui-design.md`](../ui-design.md)
already has to explain away with a mapping-table footnote.

## Decision

### 1. Data source: reuse the existing per-project GitLab connection

Merge-request sync piggybacks on the connection and link `Project` already
has ([ADR-0008](0008-why-per-project-gitlab-connection.md)) — no second
credential store, no per-user PAT. The same reasoning that put issue sync on
a project-scoped token applies here: a merge-request webhook belongs to a
GitLab project, not to whichever person happened to have a token, and a
second parallel auth model would mean deciding *twice* what a user without a
personal token can see. This supersedes the per-user-PAT expectation in
`CLAUDE.md` for this feature — that expectation predates ADR-0008 and was
never reconciled with it.

Concretely: a `Repository` (the MR-tracking object) is created for a
`LinkedGitLabProject` the moment MR sync is turned on for it, not through a
separate "add a repository" flow. One `LinkedGitLabProject` has at most one
`Repository` — same GitLab project, second facet.

### 2. Sync trigger: webhook-primary, periodic catch-up secondary

Same shape as issue sync ([ADR-0007](0007-why-outbox-worker.md)): GitLab's
`Merge Request Hook` and `Pipeline Hook` webhook events, delivered to the
existing per-link webhook endpoint, are the primary path — low latency, no
polling budget spent on quiet repositories. A periodic re-sync (reusing the
`gitlab_sync_runs`/manual-resync shape, scoped to the `Repository` instead of
the `LinkedGitLabProject`) is the fallback for missed deliveries and for
`APP_PUBLIC_URL`-less setups, exactly as issue sync already documents in
"Operating without APP_PUBLIC_URL". No new sync primitive is invented here.

### 3. Metrics: the minimal set, decided before the columns

- **Open → first review.** `merge_requests.gitlab_created_at` (already
  present, renamed from `github_created_at` in migration 000002) marks open;
  a new `first_reviewed_at` marks the first review activity (first approval
  or first review-intent note, whichever the sync engine in #111 decides
  counts — that decision belongs to #111, not this ADR).
- **First review → merge.** `first_reviewed_at` to `merged_at` (already
  present).
- **Pipeline state**, because a merge request's delivery health is
  inseparable from whether its pipeline is green: `pipeline_status`,
  `pipeline_id`, `pipeline_updated_at` track the *latest* pipeline for the
  MR's head branch, not full pipeline history — a `Pipeline` object with its
  own table is future work, added when a real design need for pipeline
  history (not just current state) shows up.
- **Merge → deploy is explicitly out of scope.** FlowLens has no deployment
  signal today (no CD integration), and inventing one to fill a column would
  be designing past the data we can actually get.

### 4. Schema realignment

- `repositories.organization_id` (→ `organizations`) is replaced with
  `repositories.linked_gitlab_project_id` (→ `linked_gitlab_projects`,
  `UNIQUE`, `ON DELETE CASCADE`) — the real parent, per §1. `organizations`
  and `organization_members` are dropped outright rather than left as unused
  placeholders: nothing references them once `repositories` points at
  `linked_gitlab_projects`, and a GitLab *group* concept isn't part of any
  current design. They can come back, differently shaped, if a real need for
  group-level rollups ever appears — reserving a table for a shape we'd
  guess at today isn't cheaper than adding one later.
- `repositories.gitlab_project_id` is dropped as a duplicate of
  `linked_gitlab_projects.gitlab_project_id` — one row now means one linked
  project, so there is exactly one place to look up its GitLab project ID.
- **Tables are renamed to match GitLab vocabulary now**, while they're still
  empty — this is the last moment it's a metadata-only change instead of a
  migration with data to move:
  - `pull_requests` → `merge_requests`
  - `pull_request_reviewers` → `merge_request_reviewers`
  - `sync_runs` → `repository_sync_runs`, so it stops being confusable with
    the unrelated, already-shipped `gitlab_sync_runs` (issue sync's own
    per-`LinkedGitLabProject` sync-run table).
  - `repositories` keeps its name — [`docs/ui-design.md`](../ui-design.md)
    already established `Repository` as this feature's noun, distinct from
    `LinkedGitLabProject`, and it already matches its table name.

  Existing GitLab-vocabulary *columns* on these tables (`gitlab_merge_request_id`,
  `author_gitlab_username`, and so on, renamed in migration 000002) are left
  as they are — this migration realigns table names and the FK hierarchy,
  not every column, to keep the change reviewable.

## Consequences

- No new credential storage, no new auth model, no second "connect GitLab"
  flow — connecting for issues is connecting for merge requests too, once MR
  sync is turned on for a linked project.
- `docs/ui-design.md`'s object-model table loses its `Organization` row and
  the footnote explaining why `Repository`/`MergeRequest`/`Reviewer` still
  carry GitHub-era table names — both artifacts of the design this ADR
  replaces.
- The rename is a one-time, data-free migration. Doing it later, once
  `merge_requests` rows exist, would mean a real data migration instead of a
  DDL rename — this is deliberately the last cheap moment.
- `Repository` becoming a required 1:1 child of `LinkedGitLabProject` means
  MR sync cannot be configured for a GitLab project FlowLens hasn't already
  linked for issue sync. That's an intentional constraint, not a gap: this
  ADR's whole premise is that there is exactly one place a GitLab project
  connection lives.
- This ADR fixes *what* is measured and *where it's stored*. It does not
  decide how "first review" is computed from GitLab's discussion/approval
  API, how backoff and dedup work for MR/pipeline webhooks, or how the
  numbers are presented — all of that is #111's design, building on the
  outbox/worker pattern ADR-0007 already established.
