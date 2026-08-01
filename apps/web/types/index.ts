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
  position: number;
  createdByUserId: string;
  createdAt: string;
  updatedAt: string;
  // null: the task's project has never had a linked GitLab project, so it is
  // purely local (see apps/api/internal/task.GitlabInfo).
  gitlab: TaskGitlabInfo | null;
  aiContext: TaskAIContext;
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
