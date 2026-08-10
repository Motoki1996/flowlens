// Server-side API client. Server Components call the Go API through the
// internal URL (the compose service name in Docker) and forward the
// browser's session cookie so the API can authenticate the request.
//
// The readers a project layout and the page inside it both need are wrapped in
// React's cache(), so rendering one screen makes one request per resource
// however many components ask for it. The rest are called from a single place
// and are left as plain functions.

import { cache } from "react";
import { cookies } from "next/headers";
import type {
  ApiToken,
  Backlog,
  GitlabConnection,
  GitlabIdentity,
  GitlabLabelOption,
  GitlabMemberOption,
  LinkedGitlabProject,
  Priority,
  Project,
  ProjectMember,
  SyncJob,
  SyncRun,
  Task,
  TaskComment,
  TaskDependency,
  TaskStatus,
  TaskWithProject,
  User,
  WebhookEventPage,
} from "@/types";

// Base URL the Next.js server uses to reach the API.
const API_INTERNAL_URL =
  process.env.API_INTERNAL_URL ?? "http://localhost:8080";

// Re-exported for server components that also need the public URL.
export { API_PUBLIC_URL } from "./config";

/**
 * getCurrentUser returns the authenticated user, or null when the request
 * has no valid session. It never throws for the unauthenticated case so
 * callers can redirect cleanly.
 */
export const getCurrentUser = cache(async (): Promise<User | null> => {
  const cookieStore = await cookies();
  const cookieHeader = cookieStore.toString();

  let res: Response;
  try {
    res = await fetch(`${API_INTERNAL_URL}/api/v1/me`, {
      headers: { cookie: cookieHeader },
      // Always hit the API; the user session must not be cached.
      cache: "no-store",
    });
  } catch {
    // API unreachable — treat as signed out at the page level.
    return null;
  }

  if (res.status === 401) return null;
  if (!res.ok) {
    throw new Error(`Failed to load current user: ${res.status}`);
  }
  return (await res.json()) as User;
});

/**
 * getProjects returns every project owned by the current user. Callers must
 * already know the request is authenticated (e.g. via getCurrentUser).
 */
export const getProjects = cache(async (): Promise<Project[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load projects: ${res.status}`);
  }
  return (await res.json()) as Project[];
});

/**
 * getFailedSyncProjects returns every project owned by the current user with
 * at least one task whose GitLab sync failed, most-recently-updated first —
 * the dashboard's "sync failures" section (issue #77). Callers must already
 * know the request is authenticated.
 */
export const getFailedSyncProjects = cache(async (): Promise<Project[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects?failedSync=true`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load failed-sync projects: ${res.status}`);
  }
  return (await res.json()) as Project[];
});

/**
 * getMyGitlabIdentities returns every GitLab identity the current user has
 * registered, one per GitLab base URL (issue #102) — /settings' identity
 * registration form. Callers must already know the request is authenticated.
 */
export const getMyGitlabIdentities = cache(async (): Promise<GitlabIdentity[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/me/gitlab-identities`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load GitLab identities: ${res.status}`);
  }
  return (await res.json()) as GitlabIdentity[];
});

/**
 * getProject returns one project, or null when it doesn't exist or isn't
 * owned by the current user (the API reports both cases as 404).
 */
export const getProject = cache(async (id: string): Promise<Project | null> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${id}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load project: ${res.status}`);
  }
  return (await res.json()) as Project;
});

/**
 * getBacklogs returns every backlog in the project, ordered by position.
 * Callers must already know the request is authenticated.
 */
export const getBacklogs = cache(async (projectId: string): Promise<Backlog[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/backlogs`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load backlogs: ${res.status}`);
  }
  return (await res.json()) as Backlog[];
});

/**
 * getBacklog returns one backlog, or null when it doesn't exist or isn't
 * owned by the current user (the API reports both cases as 404).
 */
export async function getBacklog(id: string): Promise<Backlog | null> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/backlogs/${id}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load backlog: ${res.status}`);
  }
  return (await res.json()) as Backlog;
}

/**
 * getTasks returns every task in the project, ordered by position. Callers
 * must already know the request is authenticated.
 */
export const getTasks = cache(async (projectId: string): Promise<Task[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/tasks`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load tasks: ${res.status}`);
  }
  return (await res.json()) as Task[];
});

/** AllTasksFilter is GET /api/v1/tasks's query parameters (issue #76) —
 *  every one is optional, meaning "no filter"; sort/limit are defaulted by
 *  the API itself, not here. Dates are plain YYYY-MM-DD strings — unlike a
 *  task's own dueOn/startDate body fields, these are query parameters the
 *  API parses as a bare date, not an RFC3339 timestamp (see
 *  parseDateQueryParam, internal/http/task_handler.go). */
export type AllTasksFilter = {
  status?: TaskStatus;
  priority?: Priority;
  dueBefore?: string;
  dueAfter?: string;
  startedBefore?: string;
  projectIds?: string[];
  sort?: "dueOn" | "priority" | "updatedAt";
  limit?: number;
  // "me": only tasks assigned to the caller's own registered GitLab identity
  // for that task's project (issue #102). Omitted means no filter.
  assignee?: "me";
};

/**
 * getAllTasks returns every task across every project the current user
 * owns, matching filter — the cross-project Task collection at /tasks
 * (issue #76). Callers must already know the request is authenticated.
 */
export async function getAllTasks(filter: AllTasksFilter = {}): Promise<TaskWithProject[]> {
  const cookieStore = await cookies();
  const params = new URLSearchParams();
  if (filter.status) params.set("status", filter.status);
  if (filter.priority) params.set("priority", filter.priority);
  if (filter.dueBefore) params.set("dueBefore", filter.dueBefore);
  if (filter.dueAfter) params.set("dueAfter", filter.dueAfter);
  if (filter.startedBefore) params.set("startedBefore", filter.startedBefore);
  if (filter.sort) params.set("sort", filter.sort);
  if (filter.limit) params.set("limit", String(filter.limit));
  if (filter.assignee) params.set("assignee", filter.assignee);
  for (const id of filter.projectIds ?? []) params.append("projectId", id);
  const query = params.toString();

  const res = await fetch(`${API_INTERNAL_URL}/api/v1/tasks${query ? `?${query}` : ""}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load tasks: ${res.status}`);
  }
  return (await res.json()) as TaskWithProject[];
}

/**
 * getTask returns one task including its AI context, or null when it
 * doesn't exist or isn't owned by the current user (the API reports both
 * cases as 404).
 */
export async function getTask(id: string): Promise<Task | null> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/tasks/${id}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load task: ${res.status}`);
  }
  return (await res.json()) as Task;
}

/**
 * getTaskDependencies returns every predecessor/successor dependency between
 * tasks in the project (issue #33's Gantt chart view). Callers must already
 * know the request is authenticated.
 */
export async function getTaskDependencies(projectId: string): Promise<TaskDependency[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/task-dependencies`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load task dependencies: ${res.status}`);
  }
  return (await res.json()) as TaskDependency[];
}

/**
 * getTaskComments returns a task's activity log, oldest first, with no page
 * cap (issue #103/#105). Callers must already know the request is
 * authenticated.
 */
export async function getTaskComments(taskId: string): Promise<TaskComment[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/tasks/${taskId}/comments`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load task comments: ${res.status}`);
  }
  return (await res.json()) as TaskComment[];
}

/**
 * getGitlabConnection returns the project's GitLab connection, or null when
 * none has been configured yet (the API reports both "none" and "not owned"
 * as 404).
 */
export const getGitlabConnection = cache(async (projectId: string): Promise<GitlabConnection | null> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/gitlab-connection`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load gitlab connection: ${res.status}`);
  }
  return (await res.json()) as GitlabConnection;
});

/**
 * getLinkedGitlabProjects returns every GitLab project linked to the
 * project's GitLab connection. Callers must already know the request is
 * authenticated.
 */
export const getLinkedGitlabProjects = cache(async (projectId: string): Promise<LinkedGitlabProject[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/linked-gitlab-projects`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load linked gitlab projects: ${res.status}`);
  }
  return (await res.json()) as LinkedGitlabProject[];
});

/**
 * getLinkedGitlabProject returns one of the project's linked GitLab projects,
 * or null when it doesn't exist, isn't owned by the current user, or belongs
 * to a different project (the API reports all three as 404 — a link carries
 * no project of its own in the response, which is why the project is part of
 * the route rather than something the caller checks afterwards).
 */
export async function getLinkedGitlabProject(
  projectId: string,
  linkId: string,
): Promise<LinkedGitlabProject | null> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/linked-gitlab-projects/${linkId}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load linked gitlab project: ${res.status}`);
  }
  return (await res.json()) as LinkedGitlabProject;
}

/**
 * getLinkedGitlabProjectMembers returns a linked GitLab project's members,
 * for a task's assignee picker. Callers must already know the request is
 * authenticated.
 */
export async function getLinkedGitlabProjectMembers(linkId: string): Promise<GitlabMemberOption[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/linked-gitlab-projects/${linkId}/members`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load linked gitlab project members: ${res.status}`);
  }
  const body = (await res.json()) as { members: GitlabMemberOption[]; nextPage: number };
  return body.members;
}

/**
 * getLinkedGitlabProjectLabels returns a linked GitLab project's existing
 * labels, for a task's label picker. Callers must already know the request
 * is authenticated.
 */
export async function getLinkedGitlabProjectLabels(linkId: string): Promise<GitlabLabelOption[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/linked-gitlab-projects/${linkId}/labels`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load linked gitlab project labels: ${res.status}`);
  }
  const body = (await res.json()) as { labels: GitlabLabelOption[] };
  return body.labels;
}

/**
 * getSyncRuns returns a linked GitLab project's sync run history, newest
 * first. Callers must already know the request is authenticated.
 */
export async function getSyncRuns(linkId: string): Promise<SyncRun[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/linked-gitlab-projects/${linkId}/sync-runs`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load sync runs: ${res.status}`);
  }
  return (await res.json()) as SyncRun[];
}

/**
 * getWebhookEvents returns the first page of a linked GitLab project's
 * webhook events, newest first, without their payload (GET
 * .../webhook-events only includes it in the single-event fetch). The page's
 * nextPage is carried through so the screen can offer the page after it;
 * later pages are fetched by the client component itself. Callers must
 * already know the request is authenticated.
 */
export async function getWebhookEvents(linkId: string, perPage = 10): Promise<WebhookEventPage> {
  const cookieStore = await cookies();
  const res = await fetch(
    `${API_INTERNAL_URL}/api/v1/linked-gitlab-projects/${linkId}/webhook-events?per_page=${perPage}`,
    {
      headers: { cookie: cookieStore.toString() },
      cache: "no-store",
    },
  );
  if (!res.ok) {
    throw new Error(`Failed to load webhook events: ${res.status}`);
  }
  return (await res.json()) as WebhookEventPage;
}

/**
 * getFailedSyncJobs returns a project's permanently-failed sync jobs (issue
 * #97), newest first. Callers must already know the request is
 * authenticated.
 */
export async function getFailedSyncJobs(projectId: string): Promise<SyncJob[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/sync-jobs?status=failed`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load failed sync jobs: ${res.status}`);
  }
  return (await res.json()) as SyncJob[];
}

/**
 * getProjectApiTokens returns every API token issued for the project, newest
 * first. The raw token value is never included in this listing — it is only
 * ever returned once, in the create response (see types.ApiTokenWithSecret).
 * Callers must already know the request is authenticated.
 */
export const getProjectApiTokens = cache(async (projectId: string): Promise<ApiToken[]> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/api-tokens`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load api tokens: ${res.status}`);
  }
  return (await res.json()) as ApiToken[];
});

/**
 * getProjectMembers returns every member of the project, oldest first. The
 * listing endpoint is owner-only (issue #100), so a non-owner caller gets a
 * 403 here — reported as `null` rather than thrown, since it is an expected
 * outcome the section renders a read-only state for, not a load failure.
 */
export const getProjectMembers = cache(async (projectId: string): Promise<ProjectMember[] | null> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/members`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 403) return null;
  if (!res.ok) {
    throw new Error(`Failed to load project members: ${res.status}`);
  }
  return (await res.json()) as ProjectMember[];
});
