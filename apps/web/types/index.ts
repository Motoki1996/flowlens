// Shared domain types for the web app. These mirror the API responses.

export interface User {
  id: string;
  username: string;
  email: string;
  displayName: string;
}

export interface ApiError {
  error: {
    code: string;
    message: string;
  };
}

export interface Project {
  id: string;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
  // Only populated by the single-project fetch; the collection view's list
  // fetch always reports 0 (see apps/api/internal/project.Project).
  failedSyncTaskCount: number;
}

// Priority is shared by Task and Backlog. App-only, like startDate — GitLab
// CE issues have no native priority field, so it never syncs to GitLab. A
// backlog's priority is independent of its tasks'.
export type Priority = "low" | "medium" | "high" | "urgent";

// Progress is shared by Task and Backlog: the four-stage work state FlowLens
// tracks itself. App-only like Priority, and deliberately separate from a
// task's `status` — that one is the GitLab issue state (open/closed) and syncs
// both ways, while progress never does and neither value writes the other.
export type Progress = "not_started" | "in_progress" | "on_hold" | "done";

// Size is a task's coarse estimate of how much work it is, used to weight
// velocity so throughput measures work finished rather than merely tasks
// finished. Task-only (a backlog's size would just be the sum of its tasks'),
// app-only, and never synced to GitLab. Deliberately a five-value T-shirt
// scale rather than a points field; the numeric weights (xs=1 .. xl=8) live
// server-side in apps/api/internal/velocity. "m" is the default.
export type Size = "xs" | "s" | "m" | "l" | "xl";

export interface Backlog {
  id: string;
  projectId: string;
  name: string;
  description: string;
  position: number;
  // The backlog's planned period, drawn as one bar on the Backlog timeline.
  // App-only, like a task's startDate — neither ever syncs to GitLab.
  startDate: string | null;
  dueOn: string | null;
  priority: Priority;
  progress: Progress;
  // The GitLab project a task filed in this backlog gets its issue created
  // in, overriding the project's own default link. null — the value every
  // backlog starts with — means the project default is used. Read only when a
  // task is created: moving a task between backlogs afterwards never moves an
  // issue that already exists.
  defaultLinkedGitlabProjectId: string | null;
  // The branch tasks in this backlog are meant to branch from during
  // development (e.g. "main", "release/2.4"). Optional — "" means not set.
  // App-only, never synced to GitLab.
  baseBranch: string;
  // The backlog's total and closed task counts, aggregated server-side
  // (issue #144) so the Backlog collection doesn't need to fetch every task
  // itself just to show a count and a completion ratio.
  taskCount: number;
  closedTaskCount: number;
  createdAt: string;
  updatedAt: string;
}

export type TaskStatus = "open" | "closed";

export interface TaskAIContext {
  acceptanceCriteria: string;
  aiContext: string;
  allowedScope: string;
  forbiddenScope: string;
  updatedAt: string | null;
}

export type TaskSyncStatus = "synced" | "pending" | "failed";

export interface TaskGitlabInfo {
  syncStatus: TaskSyncStatus;
  lastError: string;
  lastSyncedAt: string | null;
  issueIid: number | null;
  webUrl: string;
}

export interface Task {
  id: string;
  projectId: string;
  backlogId: string | null;
  title: string;
  description: string;
  status: TaskStatus;
  closedAt: string | null;
  assigneeGitlabUserId: number | null;
  assigneeGitlabUsername: string;
  labels: string[];
  dueOn: string | null;
  startDate: string | null;
  priority: Priority;
  progress: Progress;
  size: Size;
  // Explicit spec-driven-development phase markers, set via POST
  // .../design-started and .../implementation-started rather than derived
  // from progress transitions — see internal/flowmetrics' Design/
  // Implementation stages. App-only, never synced to GitLab.
  designStartedAt: string | null;
  implementationStartedAt: string | null;
  position: number;
  createdByUserId: string;
  createdAt: string;
  updatedAt: string;
  // null: the task's project has never had a linked GitLab project, so it is
  // purely local (see apps/api/internal/task.GitlabInfo).
  gitlab: TaskGitlabInfo | null;
  aiContext: TaskAIContext;
}

/** TaskWithProject is Task plus the name of the project it belongs to — what
 *  the cross-project Task collection at /tasks returns (GET /api/v1/tasks,
 *  issue #76), since a task alone doesn't say which project it's in. */
export interface TaskWithProject extends Task {
  projectName: string;
}

/** TaskDependency records that predecessorTaskId must finish before successorTaskId starts. */
export interface TaskDependency {
  id: string;
  predecessorTaskId: string;
  successorTaskId: string;
  createdAt: string;
}

export interface GitlabConnection {
  projectId: string;
  baseUrl: string;
  tokenLastFour: string;
  tokenGitlabUserId: number | null;
  tokenGitlabUsername: string;
  verified: boolean;
  lastVerifiedAt: string | null;
  lastVerifyError: string;
  createdAt: string;
  updatedAt: string;
}

export type SyncScope = "all" | "labels";

export type WebhookStatus = "not_registered" | "registered" | "failed";

export interface LinkedGitlabProject {
  id: string;
  gitlabConnectionId: string;
  gitlabProjectId: number;
  pathWithNamespace: string;
  name: string;
  webUrl: string;
  syncScope: SyncScope;
  syncLabels: string[];
  isDefault: boolean;
  initialImportStatus: string;
  lastSyncedAt: string | null;
  webhookStatus: WebhookStatus;
  webhookRegisteredAt: string | null;
  webhookError: string;
  createdAt: string;
  updatedAt: string;
}

/** GitlabProjectOption is one candidate returned by the available-projects search. */
export interface GitlabProjectOption {
  id: number;
  name: string;
  pathWithNamespace: string;
  webUrl: string;
}

/** GitlabMemberOption is one candidate returned by a linked GitLab project's
 *  member listing, for a task's assignee picker. */
export interface GitlabMemberOption {
  id: number;
  username: string;
  name: string;
  avatarUrl: string;
}

/** GitlabLabelOption is one candidate returned by a linked GitLab project's
 *  label listing, for a task's label picker. */
export interface GitlabLabelOption {
  name: string;
  color: string;
}

export type WebhookEventStatus = "pending" | "processed" | "skipped" | "failed";

/** WebhookEvent is one recorded GitLab webhook delivery, without its payload (see docs/plans/issue-sync.md). */
export interface WebhookEvent {
  id: string;
  linkedGitlabProjectId: string;
  eventName: string;
  objectKind: string;
  gitlabIssueIid: number | null;
  status: WebhookEventStatus;
  skipReason: string;
  errorMessage: string;
  receivedAt: string;
  processedAt: string | null;
}

/** WebhookEventDetail is one delivery *with* the raw GitLab payload, which
 *  only the single-event fetch returns (GET .../webhook-events/{eventId}). */
export interface WebhookEventDetail extends WebhookEvent {
  payload: unknown;
}

/** WebhookEventPage is one page of a link's deliveries. nextPage is 0 when
 *  there is nothing after this page. */
export interface WebhookEventPage {
  events: WebhookEvent[];
  nextPage: number;
}

export type SyncRunKind = "initial_import" | "manual_resync";

export type SyncRunStatus = "running" | "succeeded" | "failed";

/** SyncRun is one project.import/project.resync attempt against a linked GitLab project. */
export interface SyncRun {
  id: string;
  linkedGitlabProjectId: string;
  kind: SyncRunKind;
  status: SyncRunStatus;
  issuesSeen: number;
  issuesCreated: number;
  issuesUpdated: number;
  startedAt: string | null;
  completedAt: string | null;
  errorMessage: string;
  createdAt: string;
}

/** SyncJob is one permanently-failed outbox job (sync_jobs, issue #97) —
 *  a task's GitLab push that exhausted its retry budget. */
export interface SyncJob {
  id: string;
  projectId: string;
  taskId?: string;
  kind: string;
  status: string;
  attempts: number;
  lastError: string;
  runAfter: string;
  createdAt: string;
  updatedAt: string;
}

export type ApiTokenScope = "read" | "write";

/** ApiToken is a project-scoped bearer credential for the AI-facing task
 *  context API (see the "AI-facing API" section in README.md). The raw
 *  bearer value is never part of this shape — it exists only in the create
 *  response, alongside these fields (see apitoken.APIToken). */
export interface ApiToken {
  id: string;
  projectId: string;
  name: string;
  scopes: ApiTokenScope[];
  tokenPrefix: string;
  lastUsedAt: string | null;
  expiresAt: string | null;
  createdAt: string;
}

/** ApiTokenWithSecret is the create response's shape: an ApiToken plus the
 *  raw bearer value, shown exactly once. */
export interface ApiTokenWithSecret extends ApiToken {
  token: string;
}

export type ProjectMemberRole = "owner" | "member" | "viewer";

/** ProjectMember is one project_members row joined with the user it belongs
 *  to (see projectmember.Member). Email is deliberately absent — the API
 *  never returns it here, to avoid using this endpoint to enumerate
 *  registered addresses. */
export interface ProjectMember {
  userId: string;
  username: string;
  displayName: string;
  role: ProjectMemberRole;
  // True for the project's single designated owner (projects.owner_user_id),
  // who can be neither demoted nor removed. `role` alone cannot identify
  // them — any number of members may hold the "owner" role.
  isProjectOwner: boolean;
  createdAt: string;
}

/** ProjectMemberCandidate is one hit from the invite form's user search
 *  (see projectmember.Candidate). The candidates are only ever people the
 *  caller already shares a project with, minus this project's members — the
 *  API exposes no instance-wide user directory, and no email here either. */
export interface ProjectMemberCandidate {
  userId: string;
  username: string;
  displayName: string;
}

/** TaskCommentAuthorKind distinguishes who posted a task's activity-log
 *  entry: a logged-in human ("user"), a project API token ("agent"), or a
 *  GitLab discussion mirrored in by the inbound webhook ("gitlab"). */
export type TaskCommentAuthorKind = "user" | "agent" | "gitlab";

/** TaskComment is one entry in a task's activity log (issue #103/#104).
 *  Exactly one of authorUserId/authorTokenId is set, matching authorKind;
 *  both are null for a "gitlab"-authored comment. */
export interface TaskComment {
  id: string;
  taskId: string;
  authorUserId: string | null;
  authorTokenId: string | null;
  authorKind: TaskCommentAuthorKind;
  body: string;
  createdAt: string;
  updatedAt: string;
}

/** GitlabIdentity maps the caller to their GitLab user ID/username on one
 *  GitLab CE base URL (issue #102), so ?assignee=me on the task collections
 *  can match tasks assigned to them. No access token here — that is a
 *  distinct, still-unbuilt feature (ADR-0008). */
export interface GitlabIdentity {
  id: string;
  gitlabBaseUrl: string;
  gitlabUserId: number;
  gitlabUsername: string;
}

/** MergeRequestState is a GitLab CE merge request's own state — FlowLens
 *  never writes it, only mirrors it (issue #111/#112, ADR-0011). */
export type MergeRequestState = "opened" | "merged" | "closed" | "locked";

/** MergeRequest mirrors a repository's GitLab merge request, read-only
 *  (see apps/api/internal/mergerequest.MergeRequest). taskId is set when the
 *  merge request's description or branch name referenced a task's linked
 *  GitLab issue (internal/mrsync); pipelineStatus/pipelineId reflect its
 *  latest known CI pipeline. */
export interface MergeRequest {
  id: string;
  repositoryId: string;
  gitlabMergeRequestId: number;
  number: number;
  title: string;
  state: MergeRequestState;
  isDraft: boolean;
  authorGitlabUsername: string;
  authorAvatarUrl: string;
  baseBranch: string;
  headBranch: string;
  additions: number;
  deletions: number;
  changedFiles: number;
  gitlabCreatedAt: string | null;
  gitlabUpdatedAt: string | null;
  mergedAt: string | null;
  closedAt: string | null;
  htmlUrl: string;
  createdAt: string;
  updatedAt: string;
  firstReviewedAt: string | null;
  pipelineStatus: string;
  pipelineId: number | null;
  pipelineUpdatedAt: string | null;
  taskId: string | null;
}

/** DurationStats summarizes a set of durations in hours, median and p90
 *  (never a mean — lead time is reliably skewed by a handful of slow
 *  reviews). count/median/p90 are all null-safe: count is 0 and
 *  median/p90 are null when nothing in range has both timestamps the stat
 *  needs (see apps/api/internal/deliverymetrics.DurationStats). */
export interface DurationStats {
  count: number;
  medianHours: number | null;
  p90Hours: number | null;
}

/** SizeStats is DurationStats' counterpart for a plain numeric measure
 *  (additions/deletions/changed files), without the "hours" unit. */
export interface SizeStats {
  count: number;
  median: number | null;
  p90: number | null;
}

/** MetricsInterval is the ?interval= bucket size delivery/flow metrics'
 *  period time series can be grouped by (issue #188). See
 *  apps/api/internal/metricsperiod.Interval. */
export type MetricsInterval = "week" | "month" | "year";

/** DeliveryPeriodStats is the set of stats reported both for a delivery
 *  metrics request's whole range and for each of its periods — the same
 *  field list as DeliveryMetrics minus from/to/interval/truncated/periods,
 *  so the two shapes can't drift. See
 *  apps/api/internal/deliverymetrics.PeriodStats. */
export interface DeliveryPeriodStats {
  openToFirstReview: DurationStats;
  firstReviewToMerge: DurationStats;
  additions: SizeStats;
  deletions: SizeStats;
  changedFiles: SizeStats;
  /** success / (success + failed) pipelines, 0..1; null when none in range
   *  have a decided pipeline outcome. */
  pipelineSuccessRate: number | null;
  /** Count of merge requests with state "merged" in range. */
  throughput: number;
}

/** DeliveryPeriod is one interval-sized bucket of DeliveryPeriodStats
 *  (issue #188), assigned by merge_requests.gitlab_created_at. See
 *  apps/api/internal/deliverymetrics.Period. */
export interface DeliveryPeriod extends DeliveryPeriodStats {
  /** Bucket bounds in UTC; end is exclusive. */
  start: string;
  end: string;
}

/** DeliveryMetrics is a project's delivery-flow aggregation over the
 *  optional [from, to] range (issue #113, ADR-0011 §3): review/merge lead
 *  time, merge-request size distribution, pipeline success rate and
 *  throughput. See apps/api/internal/deliverymetrics.Metrics. */
export interface DeliveryMetrics extends DeliveryPeriodStats {
  from: string | null;
  to: string | null;
  /** The ?interval= the request asked to bucket periods by, or null when
   *  omitted — in which case periods is empty and truncated is false
   *  (issue #188). */
  interval: MetricsInterval | null;
  /** True when periods was capped at metricsperiod.MaxPeriods (52) and the
   *  oldest buckets were dropped. */
  truncated: boolean;
  periods: DeliveryPeriod[];
}

/** FlowPeriodStats is the set of stages reported both for a flow metrics
 *  request's whole range and for each of its periods — the same field list
 *  as FlowMetrics minus from/to/interval/truncated/periods, so the two
 *  shapes can't drift. See apps/api/internal/flowmetrics.PeriodStats. */
export interface FlowPeriodStats {
  /** backlogs.created_at -> a backlog's first transition to in_progress. */
  backlogWaitingToStart: DurationStats;
  /** A backlog's first in_progress transition -> the earliest created_at
   *  among its tasks. A backlog that already had a task filed under it
   *  before going in_progress is excluded, not counted as zero. */
  taskBreakdown: DurationStats;
  /** tasks.created_at -> the task's first transition to in_progress. */
  waitingToStart: DurationStats;
  /** task.designStartedAt -> task.implementationStartedAt — both explicit
   *  caller-set timestamps (spec-driven development), not derived from
   *  progress transitions. Excluded, not zero, for a task that never called
   *  POST .../design-started or .../implementation-started. */
  design: DurationStats;
  /** task.implementationStartedAt -> the earliest linked merge request's
   *  gitlab_created_at. */
  implementation: DurationStats;
  /** That merge request's gitlab_created_at -> merged_at. */
  reviewAndMerge: DurationStats;
  /** merged_at -> the task's first transition to done. */
  completion: DurationStats;
  /** Cumulative time across every closed on_hold interval; kept separate
   *  from the other stages so it is never double-counted against them. */
  blocked: DurationStats;
}

/** FlowPeriod is one interval-sized bucket of FlowPeriodStats (issue #188),
 *  assigned by tasks.created_at/backlogs.created_at (per stage, same as the
 *  unbucketed totals). See apps/api/internal/flowmetrics.Period. */
export interface FlowPeriod extends FlowPeriodStats {
  /** Bucket bounds in UTC; end is exclusive. */
  start: string;
  end: string;
}

/** FlowMetrics is a project's per-task stage lead-time aggregation over the
 *  optional [from, to] range, bounding tasks.created_at (issue #171): how
 *  long tasks spend waiting to start, in AI-driven implementation, in
 *  review/merge, and in completion processing, plus cumulative time spent
 *  blocked (on_hold). Each stage only counts tasks that reached both of its
 *  endpoints — see apps/api/internal/flowmetrics.Metrics. Two backlog-level
 *  stages (issue #173), bounding backlogs.created_at instead, sit one step
 *  earlier in the pipeline. */
export interface FlowMetrics extends FlowPeriodStats {
  from: string | null;
  to: string | null;
  /** The ?interval= the request asked to bucket periods by, or null when
   *  omitted — in which case periods is empty and truncated is false
   *  (issue #188). */
  interval: MetricsInterval | null;
  /** True when periods was capped at metricsperiod.MaxPeriods (52) and the
   *  oldest buckets were dropped. */
  truncated: boolean;
  periods: FlowPeriod[];
}

/** VelocityPeriod is one interval-sized bucket of completed-task throughput
 *  (issue #195), assigned by each task's *completion time* — `min(its first
 *  progress='done' transition's occurred_at, tasks.closed_at)`, whichever is
 *  non-nil — the opposite cohort basis from DeliveryPeriod/FlowPeriod, which
 *  both bucket by creation time. See apps/api/internal/velocity.Period. */
export interface VelocityPeriod {
  /** Bucket bounds in UTC; end is exclusive. */
  start: string;
  end: string;
  /** Always equal to completedByUser + completedByAgent + completedByUnknown. */
  completed: number;
  completedByUser: number;
  completedByAgent: number;
  /** A closed_at-only completion (no done-transition to read an actor from),
   *  including the pre-migration-000020 gap task_progress_events can't
   *  backfill. */
  completedByUnknown: number;
  /** `completed` weighted by each task's size; the three By* fields below
   *  split it by the same actor rule the counts use and always sum to it. */
  completedPoints: number;
  completedPointsByUser: number;
  completedPointsByAgent: number;
  completedPointsByUnknown: number;
  /** Simple average of `completed` over this period and up to 3 preceding
   *  ones (fewer once fewer exist) — the value actually meant to be read,
   *  since a single period's count is too noisy to act on alone.
   *  movingAveragePoints is the same window over completedPoints. */
  movingAverage: number;
  movingAveragePoints: number;
  /** False for a still-running period (typically the most recent one) — a
   *  partial bucket that reads low by construction, not a real slowdown. */
  complete: boolean;
}

/** Velocity is a project's completed-task throughput over the optional
 *  [from, to] range (issue #195), bounding each task's completion time, not
 *  its creation time. See apps/api/internal/velocity.Result. Unlike
 *  DeliveryMetrics/FlowMetrics, `interval` defaults to "week" server-side
 *  when omitted rather than turning off bucketing — periods are the metric
 *  here, not an optional add-on, so it is never null. */
export interface Velocity {
  from: string | null;
  to: string | null;
  interval: MetricsInterval;
  /** True when periods was capped at metricsperiod.MaxPeriods (52) and the
   *  oldest buckets were dropped. */
  truncated: boolean;
  periods: VelocityPeriod[];
  /** Current count of tasks with status='open' AND progress<>'done',
   *  regardless of from/to. */
  openTaskCount: number;
  /** Mean `completed` over the most recent up to 4 *complete* periods
   *  (excluding any still-running one); null when none is complete yet. */
  averageVelocity: number | null;
  /** openTaskCount / averageVelocity; null whenever averageVelocity is null
   *  or 0. */
  forecastPeriods: number | null;
  /** The point-denominated counterparts of the three fields above, by
   *  identical rules (averageVelocityPoints also excludes still-running
   *  periods). Once sizes are actually set these are the more trustworthy
   *  forecast, since they account for the remaining work being unusually
   *  large or small rather than assuming an average-sized task. */
  openTaskPoints: number;
  averageVelocityPoints: number | null;
  forecastPeriodsByPoints: number | null;
  /** Fraction (0..1) of the completed tasks counted here whose size is
   *  something other than the default "m". Every task predating the size
   *  column reads as "m", so while this is 0 the point series is just a 3x
   *  copy of the count series and the UI says so rather than presenting it
   *  as new information. */
  sizedTaskRatio: number;
}
