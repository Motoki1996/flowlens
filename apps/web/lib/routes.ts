/**
 * Route builders for the object hierarchy under a project. Backlogs and tasks
 * only exist inside a project, so their collection and single views are nested
 * under it (docs/ui-design.md rule 3) — keeping the paths here means a route
 * change is one edit rather than a grep across every Link.
 */

export function projectPath(projectId: string) {
  return `/projects/${projectId}`;
}

/** The cross-project Task collection (issue #76): every task across every
 *  project the user owns, "what should I be doing right now" without opening
 *  each project in turn. It has no single view of its own — each row still
 *  links to `taskPath`, the Task's one canonical single view. */
export function allTasksPath() {
  return "/tasks";
}

export function backlogsPath(projectId: string) {
  return `/projects/${projectId}/backlogs`;
}

export function backlogPath(projectId: string, backlogId: string) {
  return `/projects/${projectId}/backlogs/${backlogId}`;
}

/** The Task collection. `backlogId` pre-selects the collection's backlog
 *  filter — the backlog screens link here rather than listing tasks of their
 *  own (docs/ui-design.md rule 5). Pass UNCLASSIFIED_BACKLOG for the Unclassified
 *  group. */
export function tasksPath(projectId: string, options?: { backlogId?: string }) {
  const base = `/projects/${projectId}/tasks`;
  if (!options?.backlogId) return base;
  return `${base}?backlog=${encodeURIComponent(options.backlogId)}`;
}

/** The filter value standing for "tasks with no backlog"; shared by
 *  `tasksPath` callers and the Task collection's own filter state. */
export const UNCLASSIFIED_BACKLOG = "unclassified";

export function taskPath(projectId: string, taskId: string) {
  return `/projects/${projectId}/tasks/${taskId}`;
}

/** The MergeRequest collection (issue #112). `taskId` pre-selects the
 *  collection's task filter, the same handoff-through-the-URL pattern
 *  `tasksPath`'s `backlogId` option uses — the Task single view links here
 *  rather than growing its own MR browsing. */
export function mergeRequestsPath(projectId: string, options?: { taskId?: string }) {
  const base = `/projects/${projectId}/merge-requests`;
  if (!options?.taskId) return base;
  return `${base}?taskId=${encodeURIComponent(options.taskId)}`;
}

export function mergeRequestPath(projectId: string, mergeRequestId: string) {
  return `/projects/${projectId}/merge-requests/${mergeRequestId}`;
}

/** A project has at most one GitLab connection (ADR-0008), so this is a single
 *  view without a collection above it. */
export function gitlabConnectionPath(projectId: string) {
  return `/projects/${projectId}/gitlab-connection`;
}

export function linkedGitlabProjectPath(projectId: string, linkId: string) {
  return `/projects/${projectId}/linked-gitlab-projects/${linkId}`;
}

/**
 * The sections of a project — the entries the project sidebar shows, one per
 * collection (plus the project's own single view). A single view belongs to
 * the section of the collection above it, which is what lets the project
 * switcher keep you on the same section when you change projects.
 */
export type ProjectSection = "overview" | "backlogs" | "tasks" | "merge-requests" | "gitlab-connection";

export function projectSectionPath(projectId: string, section: ProjectSection) {
  switch (section) {
    case "backlogs":
      return backlogsPath(projectId);
    case "tasks":
      return tasksPath(projectId);
    case "merge-requests":
      return mergeRequestsPath(projectId);
    case "gitlab-connection":
      return gitlabConnectionPath(projectId);
    case "overview":
      return projectPath(projectId);
  }
}

/**
 * projectSectionOf reads the current section out of a pathname. A linked
 * GitLab project sits under the connection, so it reports "gitlab-connection";
 * anything unrecognised falls back to the project's own view.
 */
export function projectSectionOf(pathname: string): ProjectSection {
  const segment = pathname.match(/^\/projects\/[^/]+\/([^/?#]+)/)?.[1];
  switch (segment) {
    case "backlogs":
      return "backlogs";
    case "tasks":
      return "tasks";
    case "merge-requests":
      return "merge-requests";
    case "gitlab-connection":
    case "linked-gitlab-projects":
      return "gitlab-connection";
    default:
      return "overview";
  }
}
