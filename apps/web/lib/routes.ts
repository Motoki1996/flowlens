/**
 * Route builders for the object hierarchy under a project. Backlogs and tasks
 * only exist inside a project, so their collection and single views are nested
 * under it (docs/ui-design.md rule 3) — keeping the paths here means a route
 * change is one edit rather than a grep across every Link.
 */

export function projectPath(projectId: string) {
  return `/projects/${projectId}`;
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
export type ProjectSection = "overview" | "backlogs" | "tasks" | "gitlab-connection";

export function projectSectionPath(projectId: string, section: ProjectSection) {
  switch (section) {
    case "backlogs":
      return backlogsPath(projectId);
    case "tasks":
      return tasksPath(projectId);
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
    case "gitlab-connection":
    case "linked-gitlab-projects":
      return "gitlab-connection";
    default:
      return "overview";
  }
}
