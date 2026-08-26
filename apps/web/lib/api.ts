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
  DeliveryMetrics,
  Epic,
  FlowMetrics,
  MetricsInterval,
  GitlabConnection,
  GitlabIdentity,
  GitlabLabelOption,
  GitlabMemberOption,
  LinkedGitlabProject,
  MergeRequest,
  MergeRequestState,
  Priority,
  Progress,
  Project,
  Size,
  ProjectInvite,
  ProjectInvitePreview,
  ProjectMember,
  StatusFilter,
  SyncJob,
  SyncRun,
  Task,
  TaskComment,
  TaskDependency,
  TaskStatus,
  TaskWithProject,
  User,
  Velocity,
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

/** EpicListFilter is GET /api/v1/projects/{projectID}/epics's query
 *  parameters. The same shape as BacklogListFilter below, plus `backlogId`,
 *  which takes a backlog UUID or the literal "unassigned" for the epics in no
 *  backlog at all — the API spells it `backlog_id`, matching the task
 *  collection's own snake_case parameter. */
export type EpicListFilter = {
  backlogId?: string;
  /** "open", "closed", or "all". Unlike every other filter here, omitting
   *  this one is not "no filter": the API hides closed epics by default, so
   *  "all" is what a caller passes to get them back — needed wherever the
   *  result is a lookup table (resolving an epic id to a name) rather than a
   *  browsable list. */
  status?: StatusFilter;
  priority?: Priority;
  progress?: Progress;
  sort?: "priority" | "progress";
};

/**
 * getEpics returns the project's epics matching filter, in the API's default
 * (creation) order unless filter.sort says otherwise. Callers must already know the request is
 * authenticated.
 */
export const getEpics = cache(
  async (projectId: string, filter: EpicListFilter = {}): Promise<Epic[]> => {
    const cookieStore = await cookies();
    const params = new URLSearchParams();
    if (filter.status) params.set("status", filter.status);
    if (filter.backlogId) params.set("backlog_id", filter.backlogId);
    if (filter.priority) params.set("priority", filter.priority);
    if (filter.progress) params.set("progress", filter.progress);
    if (filter.sort) params.set("sort", filter.sort);
    const query = params.toString();

    const res = await fetch(
      `${API_INTERNAL_URL}/api/v1/projects/${projectId}/epics${query ? `?${query}` : ""}`,
      { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
    );
    if (!res.ok) {
      throw new Error(`Failed to load epics: ${res.status}`);
    }
    return (await res.json()) as Epic[];
  },
);

/**
 * getEpic returns one epic, or null when it doesn't exist or the current user
 * can't see it (the API reports both cases as 404).
 */
export async function getEpic(id: string): Promise<Epic | null> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/epics/${id}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load epic: ${res.status}`);
  }
  return (await res.json()) as Epic;
}

/** BacklogListFilter is GET /api/v1/projects/{projectID}/backlogs's query
 *  parameters (issue #151, mirroring ProjectTasksFilter above): every field is
 *  optional and means "no filter"/"manual order" when omitted. Unlike a
 *  task's sort, the API only orders by priority or progress — a "due date"
 *  sort is a Backlog collection concept the client applies itself (see
 *  BacklogListSection), so it never appears here. */
export type BacklogListFilter = {
  /** "open", "closed", or "all"; omitted means open-only. See
   *  EpicListFilter.status. */
  status?: StatusFilter;
  priority?: Priority;
  progress?: Progress;
  sort?: "priority" | "progress";
};

/**
 * getBacklogs returns the project's backlogs matching filter, in the API's
 * default (creation) order unless filter.sort says otherwise. Callers must already know the
 * request is authenticated.
 */
export const getBacklogs = cache(
  async (projectId: string, filter: BacklogListFilter = {}): Promise<Backlog[]> => {
    const cookieStore = await cookies();
    const params = new URLSearchParams();
    if (filter.status) params.set("status", filter.status);
    if (filter.priority) params.set("priority", filter.priority);
    if (filter.progress) params.set("progress", filter.progress);
    if (filter.sort) params.set("sort", filter.sort);
    const query = params.toString();

    const res = await fetch(
      `${API_INTERNAL_URL}/api/v1/projects/${projectId}/backlogs${query ? `?${query}` : ""}`,
      { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
    );
    if (!res.ok) {
      throw new Error(`Failed to load backlogs: ${res.status}`);
    }
    return (await res.json()) as Backlog[];
  },
);

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

/** ProjectTasksFilter is GET /api/v1/projects/{projectID}/tasks's query
 *  parameters (issue #143), built like AllTasksFilter below: every field is
 *  optional and means "no filter" when omitted. `backlogId` takes a backlog
 *  UUID or the literal "unassigned" for the Unclassified group — note the
 *  API spells that parameter `backlog_id`, the one snake_case query
 *  parameter in the API. Omitting `sort` is this list's default (creation)
 *  order, which has no named value. */
export type ProjectTasksFilter = {
  backlogId?: string;
  /** An epic UUID, or "unassigned" for the tasks sitting directly in a
   *  backlog. Spelled `epic_id` on the wire, like `backlog_id`. */
  epicId?: string;
  status?: TaskStatus;
  priority?: Priority;
  progress?: Progress;
  size?: Size;
  sort?: "dueOn" | "priority" | "progress" | "size" | "updatedAt";
  // "me": only tasks assigned to the caller's own registered GitLab identity
  // for this project's GitLab connection (issue #102, extended to this
  // screen by issue #146). Omitted means no filter.
  assignee?: "me";
  q?: string;
};

/**
 * getTasks returns the project's tasks matching filter, in the API's default
 * (creation) order unless filter.sort says otherwise. There is no pagination:
 * the project's matching tasks are returned whole, which is what lets the
 * collection group by backlog (issue #143). Callers must
 * already know the request is authenticated.
 */
export const getTasks = cache(
  async (projectId: string, filter: ProjectTasksFilter = {}): Promise<Task[]> => {
    const cookieStore = await cookies();
    const params = new URLSearchParams();
    if (filter.backlogId) params.set("backlog_id", filter.backlogId);
    if (filter.epicId) params.set("epic_id", filter.epicId);
    if (filter.status) params.set("status", filter.status);
    if (filter.priority) params.set("priority", filter.priority);
    if (filter.progress) params.set("progress", filter.progress);
    if (filter.size) params.set("size", filter.size);
    if (filter.sort) params.set("sort", filter.sort);
    if (filter.assignee) params.set("assignee", filter.assignee);
    if (filter.q) params.set("q", filter.q);
    const query = params.toString();

    const res = await fetch(
      `${API_INTERNAL_URL}/api/v1/projects/${projectId}/tasks${query ? `?${query}` : ""}`,
      { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
    );
    if (!res.ok) {
      throw new Error(`Failed to load tasks: ${res.status}`);
    }
    return (await res.json()) as Task[];
  },
);

/** AllTasksFilter is GET /api/v1/tasks's query parameters (issue #76) —
 *  every one is optional, meaning "no filter"; sort/limit are defaulted by
 *  the API itself, not here. Dates are plain YYYY-MM-DD strings — unlike a
 *  task's own dueOn/startDate body fields, these are query parameters the
 *  API parses as a bare date, not an RFC3339 timestamp (see
 *  parseDateQueryParam, internal/http/task_handler.go). */
export type AllTasksFilter = {
  status?: TaskStatus;
  priority?: Priority;
  progress?: Progress;
  size?: Size;
  dueBefore?: string;
  dueAfter?: string;
  startedBefore?: string;
  projectIds?: string[];
  sort?: "dueOn" | "priority" | "progress" | "size" | "updatedAt";
  limit?: number;
  // "me": only tasks assigned to the caller's own registered GitLab identity
  // for that task's project (issue #102). Omitted means no filter.
  assignee?: "me";
  // Free-text match against a task's title or description (issue #106's
  // `?q=`, surfaced in the UI by issue #107).
  q?: string;
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
  if (filter.progress) params.set("progress", filter.progress);
  if (filter.size) params.set("size", filter.size);
  if (filter.dueBefore) params.set("dueBefore", filter.dueBefore);
  if (filter.dueAfter) params.set("dueAfter", filter.dueAfter);
  if (filter.startedBefore) params.set("startedBefore", filter.startedBefore);
  if (filter.sort) params.set("sort", filter.sort);
  if (filter.limit) params.set("limit", String(filter.limit));
  if (filter.assignee) params.set("assignee", filter.assignee);
  if (filter.q) params.set("q", filter.q);
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

/** MergeRequestFilter is GET .../merge-requests's query parameters (issue
 *  #112) — every one is optional, meaning "no filter". since/until are plain
 *  YYYY-MM-DD strings, the same date-only convention AllTasksFilter's
 *  dueBefore/dueAfter use. taskId lets the Task single view fetch its own
 *  related merge requests through this same endpoint. page/perPage are the
 *  API's 1-based paging: a repository synced for a year holds thousands of
 *  merged merge requests, so the endpoint never returns all of them at once. */
export type MergeRequestFilter = {
  state?: MergeRequestState;
  author?: string;
  taskId?: string;
  since?: string;
  until?: string;
  sort?: "updated";
  page?: number;
  perPage?: number;
};

/** MergeRequestsPage is GET .../merge-requests's response envelope:
 *  nextPage is 0 when no further page follows, and totalCount is how many
 *  merge requests match the filter across every page. */
export type MergeRequestsPage = {
  mergeRequests: MergeRequest[];
  nextPage: number;
  totalCount: number;
};

/**
 * getMergeRequests returns one page of the project's merge requests matching
 * filter, newest (by GitLab creation date) first unless filter.sort overrides
 * it. Callers must already know the request is authenticated.
 */
export async function getMergeRequests(
  projectId: string,
  filter: MergeRequestFilter = {},
): Promise<MergeRequestsPage> {
  const cookieStore = await cookies();
  const params = new URLSearchParams();
  if (filter.state) params.set("state", filter.state);
  if (filter.author) params.set("author", filter.author);
  if (filter.taskId) params.set("taskId", filter.taskId);
  if (filter.since) params.set("since", filter.since);
  if (filter.until) params.set("until", filter.until);
  if (filter.sort) params.set("sort", filter.sort);
  if (filter.page) params.set("page", String(filter.page));
  if (filter.perPage) params.set("per_page", String(filter.perPage));
  const query = params.toString();

  const res = await fetch(
    `${API_INTERNAL_URL}/api/v1/projects/${projectId}/merge-requests${query ? `?${query}` : ""}`,
    { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
  );
  if (!res.ok) {
    throw new Error(`Failed to load merge requests: ${res.status}`);
  }
  return (await res.json()) as MergeRequestsPage;
}

/**
 * getMergeRequest returns one merge request, or null when it doesn't exist
 * or isn't visible to the current user (the API reports both cases as 404).
 */
export async function getMergeRequest(id: string): Promise<MergeRequest | null> {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/merge-requests/${id}`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load merge request: ${res.status}`);
  }
  return (await res.json()) as MergeRequest;
}

/** ProjectMetricsFilter narrows getProjectMetrics to a date range (issue
 *  #113); both bounds are optional YYYY-MM-DD strings, unbounded when
 *  omitted, the same date-only convention MergeRequestFilter's since/until
 *  use. interval (issue #188) additionally buckets the response into a
 *  periods time series; omitted, the response is unchanged from before #188. */
export type ProjectMetricsFilter = {
  from?: string;
  to?: string;
  interval?: MetricsInterval;
};

/**
 * getProjectMetrics returns the project's delivery-flow metrics (issue
 * #113): review/merge lead time, merge-request size distribution, pipeline
 * success rate and throughput. Callers must already know the request is
 * authenticated.
 */
export async function getProjectMetrics(
  projectId: string,
  filter: ProjectMetricsFilter = {},
): Promise<DeliveryMetrics> {
  const cookieStore = await cookies();
  const params = new URLSearchParams();
  if (filter.from) params.set("from", filter.from);
  if (filter.to) params.set("to", filter.to);
  if (filter.interval) params.set("interval", filter.interval);
  const query = params.toString();

  const res = await fetch(
    `${API_INTERNAL_URL}/api/v1/projects/${projectId}/metrics${query ? `?${query}` : ""}`,
    { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
  );
  if (!res.ok) {
    throw new Error(`Failed to load project metrics: ${res.status}`);
  }
  return (await res.json()) as DeliveryMetrics;
}

/**
 * getProjectFlowMetrics returns the project's per-task stage lead-time
 * aggregation (issue #171): waiting-to-start/implementation/review-and-merge/
 * completion/blocked durations, over the same optional [from, to] range
 * shape as getProjectMetrics. Callers must already know the request is
 * authenticated.
 */
export async function getProjectFlowMetrics(
  projectId: string,
  filter: ProjectMetricsFilter = {},
): Promise<FlowMetrics> {
  const cookieStore = await cookies();
  const params = new URLSearchParams();
  if (filter.from) params.set("from", filter.from);
  if (filter.to) params.set("to", filter.to);
  if (filter.interval) params.set("interval", filter.interval);
  const query = params.toString();

  const res = await fetch(
    `${API_INTERNAL_URL}/api/v1/projects/${projectId}/flow-metrics${query ? `?${query}` : ""}`,
    { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
  );
  if (!res.ok) {
    throw new Error(`Failed to load project flow metrics: ${res.status}`);
  }
  return (await res.json()) as FlowMetrics;
}

/**
 * getProjectVelocity returns the project's completed-task throughput
 * aggregation (issue #195), over the same optional [from, to] range shape as
 * getProjectMetrics/getProjectFlowMetrics — but bucketed by each task's
 * *completion* time, not creation time. Callers must already know the
 * request is authenticated.
 */
export async function getProjectVelocity(
  projectId: string,
  filter: ProjectMetricsFilter = {},
): Promise<Velocity> {
  const cookieStore = await cookies();
  const params = new URLSearchParams();
  if (filter.from) params.set("from", filter.from);
  if (filter.to) params.set("to", filter.to);
  if (filter.interval) params.set("interval", filter.interval);
  const query = params.toString();

  const res = await fetch(
    `${API_INTERNAL_URL}/api/v1/projects/${projectId}/velocity${query ? `?${query}` : ""}`,
    { headers: { cookie: cookieStore.toString() }, cache: "no-store" },
  );
  if (!res.ok) {
    throw new Error(`Failed to load project velocity: ${res.status}`);
  }
  return (await res.json()) as Velocity;
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
 * getProjectInvites returns every invite issued for the project, newest
 * first — including spent and expired ones, so an owner can audit who was
 * let in. Owner-only like the member listing, so a non-owner's 403 is
 * reported as `null` rather than thrown.
 */
export const getProjectInvites = cache(async (projectId: string): Promise<ProjectInvite[] | null> => {
  const cookieStore = await cookies();
  const res = await fetch(`${API_INTERNAL_URL}/api/v1/projects/${projectId}/invites`, {
    headers: { cookie: cookieStore.toString() },
    cache: "no-store",
  });
  if (res.status === 403 || res.status === 404) return null;
  if (!res.ok) {
    throw new Error(`Failed to load project invites: ${res.status}`);
  }
  return (await res.json()) as ProjectInvite[];
});

/**
 * getInvitePreview resolves an invite token to the project and role it
 * grants. Unauthenticated by design — the person it is for may have no
 * account yet — so no cookie is forwarded. `null` covers every reason the
 * invite cannot be accepted; the API deliberately does not distinguish
 * unknown from expired from already-used.
 */
export async function getInvitePreview(token: string): Promise<ProjectInvitePreview | null> {
  const res = await fetch(`${API_INTERNAL_URL}/auth/invites/${encodeURIComponent(token)}`, {
    cache: "no-store",
  });
  if (!res.ok) return null;
  return (await res.json()) as ProjectInvitePreview;
}

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
