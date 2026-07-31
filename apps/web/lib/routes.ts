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

export function tasksPath(projectId: string) {
  return `/projects/${projectId}/tasks`;
}

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
