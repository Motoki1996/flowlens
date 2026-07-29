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
}

export interface Backlog {
  id: string;
  projectId: string;
  name: string;
  description: string;
  position: number;
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
  position: number;
  createdByUserId: string;
  createdAt: string;
  updatedAt: string;
  // gitlab carries GitLab issue sync fields once that feature ships; always
  // null until then (see apps/api/internal/task.GitlabInfo).
  gitlab: Record<string, never> | null;
  aiContext: TaskAIContext;
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
