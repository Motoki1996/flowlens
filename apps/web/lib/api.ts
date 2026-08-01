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
  Backlog,
  GitlabConnection,
  LinkedGitlabProject,
  Project,
  SyncRun,
  Task,
  TaskDependency,
  User,
  WebhookEvent,
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
 * getWebhookEvents returns a linked GitLab project's most recently received
 * webhook events, newest first, without their payload (GET
 * .../webhook-events only includes it in the single-event fetch). Callers
 * must already know the request is authenticated.
 */
export async function getWebhookEvents(linkId: string, perPage = 10): Promise<WebhookEvent[]> {
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
  const body = (await res.json()) as { events: WebhookEvent[]; nextPage: number };
  return body.events;
}
