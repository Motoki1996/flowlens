# Delivery, flow and velocity metrics

## Delivery metrics (issue #113)

The merge-request / CI delivery-flow metrics [ADR-0011](../decisions/0011-why-merge-request-sync.md)
designed and [#111](merge-requests.md#merge-request-sync-issue-111)/[#112](merge-requests.md#merge-request-views-issue-112)
populated, finally aggregated and charted — this is what makes FlowLens live
up to its name. Read-only, computed on request from `merge_requests` already
synced; nothing is cached or materialized yet.

- `GET /api/v1/projects/{projectID}/metrics?from=&to=` (`YYYY-MM-DD`, both
  optional, bounding `gitlab_created_at` the same way the merge-request
  collection's `?since=`/`?until=` do) returns:
  - **Open → first review** and **first review → merge** durations, each as
    a **median and p90** (never a mean — lead time is reliably skewed by a
    handful of slow reviews, and a mean lets outliers hide behind a falsely
    comfortable "average"). A merge request missing either timestamp (not
    yet reviewed, not yet merged) is excluded from that stat rather than
    counted as zero.
  - **Merge-request size distribution** (median/p90 of `additions`/
    `deletions`/`changed_files`) — the columns already exist on
    `merge_requests`, but `internal/mrsync` doesn't fetch GitLab's diff
    stats yet, so every merge request's size is 0 today; this aggregation is
    ready for when a future issue backfills them.
  - **Pipeline success rate**: `success ÷ (success + failed)` pipelines;
    `null` when nothing in range has a decided outcome (still
    running/pending/skipped/canceled/manual/no pipeline don't count toward
    either side).
  - **Throughput**: count of merge requests with state `merged` in range.
  - Session-only, not on the bearer-token allowlist — this is a chart for a
    human reading the Project view, not an AI-facing read.
  - Median/p90 use the nearest-rank method (no interpolation) — see
    `apps/api/internal/deliverymetrics`.
  - `&interval=week|month|year` (issue #188) additionally buckets the same
    stats into a `periods` time series, so "is this improving?" is readable
    alongside "how is it now?" — see [Period bucketing](#period-bucketing-issue-188)
    below; the rules there (cohort basis, UTC/ISO week, gap-fill, 52-period
    cap) apply identically here, bucketing by `merge_requests.gitlab_created_at`.
- Web: a stat row (throughput, pipeline success rate) on the "Delivery
  metrics" card on the Project single view (`/projects/[projectId]`), with
  `?from=`/`?to=` date filters held in the URL. The open→first-review/
  first-review→merge lead time is no longer charted on its own here — see
  [Flow metrics](#flow-metrics-issue-171)'s `reviewAndMerge` stage, which the
  same card now charts instead (issue #172). Size distribution isn't charted
  yet — see above. With `?interval=` selected (issue #189, an `All`/`Week`/
  `Month`/`Year` selector next to the date filters), the stat row gains a
  small throughput bar chart and pipeline-success-rate line chart underneath,
  one point per period.
- The aggregation started as a plain query over `merge_requests`, computing
  median/p90 in the application layer (cheap to unit test with fakes, per
  [`docs/testing.md`](../testing.md)); a materialized view is future work
  if `EXPLAIN` on real data ever calls for one, not before.

## Flow metrics (issue #171)

Once the `task_progress_events` log the
[progress convention for agents](agents.md#progress-convention-for-agents-issue-170)
populates has real history in it, it can be aggregated into per-stage lead
time — the work `internal/flowmetrics` does, in the same read-only,
compute-on-request shape as [Delivery metrics](#delivery-metrics-issue-113).
Where delivery metrics look only at `merge_requests`, flow metrics walk a
task's whole pipeline: from creation, through an agent picking it up,
through the merge request that closes it out, to the task being marked
done.

- `GET /api/v1/projects/{projectID}/flow-metrics?from=&to=&interval=` (`from`/
  `to` as `YYYY-MM-DD`, both optional, bounding `tasks.created_at`;
  `interval` covered in [Period bucketing](#period-bucketing-issue-188)
  below) returns six stages, each as a
  **median and p90** in hours (never a mean, same rationale as delivery
  metrics) over only the tasks that reached *both* ends of that stage — a
  task that hasn't reached a stage's end yet is excluded from it rather than
  counted as a zero duration:
  - **`waitingToStart`**: `tasks.created_at` → the task's first transition
    to `in_progress`.
  - **`design`**: `tasks.design_started_at` → `tasks.implementation_started_at`
    (migration `000023`). Unlike every other stage here, these two
    timestamps aren't derived from `task_progress_events` — they're written
    directly by whoever starts that phase (an AI agent doing spec-driven
    development, or a human) via `POST /api/v1/tasks/{taskID}/design-started`
    and `POST /api/v1/tasks/{taskID}/implementation-started`. Both endpoints
    are session- and bearer-token-writable (`write` scope), **always
    overwrite** — unlike most task fields there is no "already set" guard,
    so redoing the design after review feedback just moves the timestamp
    forward — and are independent of each other: a task with
    `implementation_started_at` but no `design_started_at` simply skipped
    the design phase and is excluded from `design` (not counted as zero)
    while still counting toward `implementation`. A task that never calls
    either endpoint has no `design`/`implementation` sample at all — this
    pair is opt-in, unlike every other stage, which needs no extra call.
  - **`implementation`**: `tasks.implementation_started_at` → the earliest
    linked merge request's `gitlab_created_at`. A task with more than one
    linked merge request (a follow-up MR, say) is measured against the
    earliest one, since that's the one that actually closed the wait.
  - **`reviewAndMerge`**: that merge request's `gitlab_created_at` →
    `merged_at` — one span, unlike delivery metrics' open→first-review/
    first-review→merge split; `first_reviewed_at` isn't used here.
  - **`completion`**: `merged_at` → the task's first transition to `done`.
  - **`blocked`**: cumulative time across every *closed* `on_hold`
    interval a task passed through (entering and later leaving, however
    many round trips); a task still `on_hold` with no exit yet has that
    stretch excluded, and a task never `on_hold` at all is excluded
    entirely rather than counted as zero.
  - A task with no linked merge request (no code change involved — a
    research spike, a docs task) is excluded from `implementation` and
    `reviewAndMerge` by the same "both ends known" rule. **This is
    intentional, not a bug**: those two stages measure code-review lead
    time, which doesn't exist for a task that never produced a merge
    request.
  - Session-only, not on the bearer-token allowlist — like delivery
    metrics, this is a chart for a human reading the Project view, not an
    AI-facing read.
- Median/p90 use the same nearest-rank method as delivery metrics —
  currently duplicated in `apps/api/internal/flowmetrics` rather than
  shared, since the two aggregations are still small and independent.
- Web (issue #172, narrowed to Design-onward by the `design`/`implementation`
  split above): the same "Delivery metrics" card on the Project single view
  charts `design`, `implementation`, `reviewAndMerge` and `completion` as a
  stacked horizontal bar — a value-stream map, so the tallest segment reads
  as the bottleneck at a glance. `waitingToStart` and the two backlog-level
  stages below are still returned by the API but are not part of this
  chart. `blocked` is charted separately from that stack, never folded into
  it, so blocked time is never double-counted against the stage it
  interrupted. Median and p90 switch via a shared tab above both charts
  (issue #189) rather than drawing two rows at once — one piece of state for
  both, since letting them switch independently would invite reading one
  chart's median against the other's p90. It shares the card's
  `?from=`/`?to=` filters with delivery metrics.

### Backlog-level stages: waiting to start and task breakdown (issue #173)

A task's whole pipeline above still starts at `tasks.created_at` — it says
nothing about how long a backlog sat before anyone started it, or how long
it took to break that backlog down into tasks once someone did. `#169`'s
`task_progress_events` can't answer either question, since neither happens
to a task. `backlog_progress_events` (migration `000022`) is the
backlog-level counterpart, an append-only log of `backlogs.progress`
transitions in the same shape, written from the single insertion point
`internal/backlog.Service.Update` — only when `progress` actually changes,
attributed to `actor_kind`/`actor_user_id` exactly like a task's own
(`"agent"` for a bearer-token caller, `"user"` for a session caller, since
`PATCH /backlogs/{backlogID}` is on the same shared allowlist as a task's).

`GET /api/v1/projects/{projectID}/flow-metrics?from=&to=` returns two more
stages alongside the five above, this time bounding `backlogs.created_at`
rather than `tasks.created_at`:

- **`backlogWaitingToStart`**: `backlogs.created_at` → a backlog's first
  transition to `in_progress`.
- **`taskBreakdown`**: that same transition → the earliest `created_at`
  among the backlog's tasks — the AI-driven "break this backlog into tasks"
  step this flow means to measure. A backlog that already had a task filed
  under it *before* going `in_progress` is excluded from this stage
  entirely rather than counted as a zero (or negative) duration: there was
  no breakdown work left to time after the transition.

Both follow the same "both ends known, excluded rather than zero" rule as
every other stage. Both are returned by the API but, like `waitingToStart`,
are not part of the web stage-lead-time chart — that chart starts at
`design`, per the [design/implementation split](#flow-metrics-issue-171)
above. `blocked` is unaffected and stays its own separate chart.

### Period bucketing (issue #188)

A single `?from=&to=` range says "how is it now?" but not "is it
improving?" — `&interval=week|month|year`, accepted by both
[Delivery metrics](#delivery-metrics-issue-113) and flow metrics above, adds
a `periods` time series to the response without changing any existing
field: **omitted, the response is byte-for-byte what it was before this
issue**, `"interval"` is `null`, and `"periods"` is empty.

- **Cohort basis, not event basis.** A period is chosen by when the row was
  *created*, not by when the value being measured happened — the same
  `?from=/?to=` bound each endpoint already filters by:
  - Flow metrics' task-level stages (`waitingToStart`/`design`/
    `implementation`/`reviewAndMerge`/`completion`/`blocked`): `tasks.created_at`.
  - Flow metrics' backlog-level stages (`backlogWaitingToStart`/
    `taskBreakdown`): `backlogs.created_at`.
  - Delivery metrics (all fields): `merge_requests.gitlab_created_at`.

  This means a period reports "how long did the cohort *created* in this
  window end up taking", not "what happened during this window" — the most
  recent periods are thinner on data because much of that cohort hasn't
  finished yet. That's accepted deliberately (see issue #188): it keeps
  every row in exactly one period and keeps each period's median/p90
  computed from a consistent, non-overlapping sample.
- **UTC, ISO week.** Every boundary is UTC, matching `timestamptz` storage
  and every other aggregation here. A `week` bucket starts Monday 00:00 UTC
  (so a Sunday timestamp belongs to the *previous* Monday's week); `month`
  starts the 1st 00:00 UTC; `year` starts Jan 1 00:00 UTC. `end` on every
  period is exclusive.
- **Gap-filled.** Every bucket between the covered range's start and end is
  present with `count: 0`, even ones with no data at all — so a chart can
  render a flat/empty period instead of skipping straight to the next one
  with data. The covered range is `from`→`to` when given; a missing bound
  falls back to the earliest/latest bucket actually observed in the
  response's own data.
- **Capped at 52 periods.** An unbounded `?interval=week` could otherwise
  return hundreds of rows. Once the covered range would exceed 52 buckets,
  only the newest 52 are returned and the response's top-level
  `"truncated"` is `true`.
- An unrecognized `interval` value is a 400, the same treatment as a
  malformed `from`/`to`.
- Shared boundary math (`BucketStart`/`BucketEnd`/the gap-fill+cap
  `Timeline`) lives in `apps/api/internal/metricsperiod`, used by both
  `internal/flowmetrics` and `internal/deliverymetrics` — bucket-boundary
  bugs are the kind worth fixing in one place, unlike the median/p90 helpers
  those two packages still duplicate.
- Web (issue #189): an `All`/`Week`/`Month`/`Year` selector next to the
  "Delivery metrics" card's date filters, held in the URL as `?interval=`
  alongside `?from=`/`?to=` (server-refetched, not client-recomputed —
  the same hand-off-through-the-URL pattern the date filters already use).
  With an interval selected:
  - Stage lead time and Blocked time each draw one horizontal stacked-bar row
    per period instead of one summary row, oldest period on top/newest on
    bottom — reading top-to-bottom shows whether lead time is shrinking. A
    period with `count: 0` still draws its (empty) row, so a gap in the data
    reads as a gap rather than silently disappearing. `"truncated": true`
    shows a one-line note that only the most recent 52 periods are shown.
  - The stat row gains a small throughput bar chart and pipeline-success-rate
    line chart underneath, one point per period.
  - `All` (the default, no `interval` in the URL) is unchanged from before
    this issue except that Stage/Blocked draw only the tab-selected stat's
    row, not both at once — see [Flow metrics](#flow-metrics-issue-171)'s
    Median/p90 tab above.

## Velocity (issue #195)

Throughput — completed tasks per period — as distinct from
[Delivery metrics](#delivery-metrics-issue-113) and
[Flow metrics](#flow-metrics-issue-171), which both measure how long one
item took, not how many finished in a window. It is reported two ways at
once: a raw completed-task count, and a total weighted by each task's
[size](tasks.md#task-size). Both are split by `task_progress_events.actor_kind` into
user/agent/unknown, a breakdown no story-point tool can give, since "how
much throughput did the agent actually produce" only means something once
agents are doing the work.

The two units answer different questions and neither is redundant: a count
alone can be inflated for free by splitting tasks smaller, while points
alone hide whether the work arrived as a few large items or many small ones.
There is still deliberately no story-point/estimate concept — no sprint or
timebox for a "points per sprint" figure to hang off, and no number anyone
types per task; `size` is a five-value T-shirt scale and the weights
(`xs`=1 … `xl`=8) live in `internal/velocity`.

- A task's **completion time** is `min(its first progress='done'
  transition's occurred_at, tasks.closed_at)`, whichever is non-nil; a task
  with neither is not completed and is never counted. Both signals have to
  be checked: `tasks.progress` is app-only and GitLab sync does not write it
  by default, so a task closed on the GitLab side alone never reaches
  `progress='done'` and would be invisible if only `task_progress_events`
  were read — unless [progress sync on issue close](tasks.md#task--backlog-progress)
  is turned on for the project, in which case that same GitLab-side close
  is exactly what writes `progress='done'`, via an `actor_kind = "gitlab"`
  event;
  conversely `tasks.status` can stay `open` after `progress` reaches
  `done` (they're separate axes that never write each other), so
  `closed_at` alone would miss those. Each task counts at most once, at
  the earlier of the two — a tie prefers the `done` transition, since it
  alone carries an actor. `task_progress_events` only exists from migration
  `000020` on (issue #169); a task done before that migration shipped has
  no event row and is only reachable via `closed_at`, with no actor
  breakdown — that gap can't be backfilled and is expected, not a bug.
- `GET /api/v1/projects/{projectID}/velocity?from=&to=&interval=` (`from`/
  `to` as `YYYY-MM-DD`, both optional) buckets tasks by their **completion
  time**, not their `created_at` — the opposite cohort basis from delivery/
  flow metrics above, since this endpoint answers "how much finished in
  this window", not "how is the cohort created in this window doing".
  `from`/`to` bound completion time the same way. `interval` follows
  [Period bucketing](#period-bucketing-issue-188)'s UTC/ISO-week
  boundaries, gap-fill and 52-period cap, but **defaults to `week`** when
  omitted rather than "don't bucket" — periods are the metric here, not an
  optional add-on. Session-only, not on the bearer-token allowlist, like
  the other two metrics endpoints. Each period reports:
  - `completed`, split into `completedByUser`/`completedByAgent`/
    `completedByUnknown` (always summing back to `completed`) —
    `completedByUnknown` covers a `closed_at`-only completion (no actor to
    read) and the pre-migration-000020 gap above.
  - `movingAverage`: the simple average of `completed` over this period and
    up to 3 preceding ones (fewer once fewer exist) — a single period's
    count is too noisy to act on alone; this is the value meant to
    actually be read.
  - `complete`: `false` for a still-running period (typically the most
    recent one), so a chart can tell a partial bucket apart from a
    finished one.
  - `completedPoints`, split the same three ways and by the same actor rule,
    weighting each completed task by its size.
  - The response also reports `openTaskCount` (current
    `status='open' AND progress<>'done'` count, regardless of `from`/`to`),
    `averageVelocity` (the mean `completed` over the most recent up to 4
    **complete** periods — excluding any still-running period, which would
    otherwise understate velocity by construction — `null` if none is
    complete yet), and `forecastPeriods` (`openTaskCount / averageVelocity`,
    `null` whenever that's `null` or `0`): how many more periods, at the
    recent pace, the remaining open tasks would take.
  - `openTaskPoints`, `averageVelocityPoints` and `forecastPeriodsByPoints`
    are the point-denominated counterparts of those three, by identical
    rules — `averageVelocityPoints` also excludes still-running periods.
    Once sizes are actually set, the point forecast is the more trustworthy
    of the two, since it accounts for the remaining work being unusually
    large or small instead of assuming an average-sized task.
  - `unbrokenDownEpicPoints`, `unestimatedEpicCount` and `openPointsTotal`
    (issue #234) carry the remaining work that has no tasks yet:
    [epics that have not been broken down](epics.md#an-epics-provisional-estimate-issue-234),
    through their `estimatedPoints`. An epic that *does* have tasks
    contributes nothing here — its work is already in `openTaskPoints`, task
    by task, and adding its estimate on top would count it twice.
    `openPointsTotal` is `openTaskPoints + unbrokenDownEpicPoints` and is the
    numerator of `forecastPeriodsByPoints`.
    `unestimatedEpicCount` is how many of those epics nobody has estimated:
    they add `0` because there is nothing to add, *not* because they are no
    work, so a nonzero value means the forecast is a lower bound — reporting
    it is the point, since silently treating an unestimated epic as zero is
    the bug this replaced. The count series (`forecastPeriods`,
    `openTaskCount`) deliberately stays task-only: an epic has no idea how
    many tasks it will become, so there is no honest number to add to a
    count — which is why the estimate is denominated in points at all.

    One known understatement remains, deliberately: "has been broken down" is
    all-or-nothing, so an epic estimated at 21 points with a single `xs` task
    cut off it leaves this figure entirely and contributes 1 point through
    that task — the ~20 points still to be cut are invisible until the
    breakdown finishes. Reconciling the estimate against the tasks (a `max()`,
    a remainder) would fix the number and reintroduce the two-disagreeing-
    truths problem the epic layer exists to avoid ([ADR-0012](../decisions/0012-why-an-epic-layer.md)),
    so it is left understated; the window is short by construction, since
    `/flowlens:breakdown` creates an epic's tasks in one bulk call. The
    Velocity card says when the points forecast is a lower bound rather than
    presenting it as a point estimate.
  - `sizedTaskRatio` (0..1) is the fraction of the completed tasks counted
    whose size is something other than the default `m`. Every task predating
    the `size` column reads as `m`, so while this is `0` the point series is
    arithmetically 3x the count series and carries no extra information —
    the web card says so rather than presenting a rescaled duplicate as a
    second opinion.
- Web (issue #196): a "Velocity" card on the Project single view, placed
  immediately *before* the "Delivery metrics" card so velocity reads
  alongside lead time rather than as a screen of its own — there is
  deliberately no standalone `/velocity` screen, since throughput alone is
  easy to game (splitting tasks smaller inflates it for free) and only means
  something read next to lead time staying flat or improving. It shares
  "Delivery metrics"' `?from=&to=&interval=` URL filter rather than exposing
  a second selector; unlike that card, it always draws one bar per period
  (defaulting to `week` when `interval` is omitted, per the API default
  above).
  - A stacked bar per period: `completedByUser`/`completedByAgent`/
    `completedByUnknown`, in that order both in the stack and in the legend.
    `movingAverage` is overlaid as a line on the same chart, since a single
    period's bar is too noisy to read on its own.
  - A `Tasks`/`Points` tab switches bars, moving average and both stats
    together (never a mix) between the count and the size-weighted series.
    Both arrive pre-weighted from the API; the client never multiplies
    anything itself. On the Points tab, a project where no completed task has
    been sized yet gets a one-line note saying the series is just the task
    count x 3.
  - A still-running period (`complete: false`) draws its bars at reduced
    opacity, so a partial bucket is never misread as a slowdown.
  - `averageVelocity`/`forecastPeriods` are shown as a small stat row (e.g.
    "9.5 tasks/week" / "34 open ≈ 3.6 weeks left"); either being `null` shows
    a placeholder instead of a number.
  - A project with no completed tasks yet shows "No completed tasks yet."
    instead of an empty chart.

