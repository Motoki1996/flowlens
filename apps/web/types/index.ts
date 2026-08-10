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
  createdAt: string;
}
