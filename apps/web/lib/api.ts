// Server-side API client. Server Components call the Go API through the
// internal URL (the compose service name in Docker) and forward the
// browser's session cookie so the API can authenticate the request.

import { cookies } from "next/headers";
import type { Backlog, Project, Task, User } from "@/types";

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
export async function getCurrentUser(): Promise<User | null> {
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
}

/**
 * getProjects returns every project owned by the current user. Callers must
 * already know the request is authenticated (e.g. via getCurrentUser).
 */
export async function getProjects(): Promise<Project[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load projects: ${res.status}`);
  }
  return (await res.json()) as Project[];
}

/**
 * getProject returns one project, or null when it doesn't exist or isn't
 * owned by the current user (the API reports both cases as 404).
 */
export async function getProject(id: string): Promise<Project | null> {
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
}

/**
 * getBacklogs returns every backlog in the project, ordered by position.
 * Callers must already know the request is authenticated.
 */
export async function getBacklogs(projectId: string): Promise<Backlog[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/backlogs`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load backlogs: ${res.status}`);
  }
  return (await res.json()) as Backlog[];
}

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
export async function getTasks(projectId: string): Promise<Task[]> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/tasks`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (!res.ok) {
    throw new Error(`Failed to load tasks: ${res.status}`);
  }
  return (await res.json()) as Task[];
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
