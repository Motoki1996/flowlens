import { notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getLinkedGitlabProjectLabels,
  getLinkedGitlabProjectMembers,
  getLinkedGitlabProjects,
  getMergeRequests,
  getProject,
  getProjectApiTokens,
  getTask,
  getTaskComments,
  getTaskDependencies,
  getTasks,
} from "@/lib/api";
import { tasksPath } from "@/lib/routes";
import type { ApiToken, GitlabLabelOption, GitlabMemberOption } from "@/types";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { TaskDetail } from "@/components/TaskDetail";

// Auth is guarded by the parent layout.tsx (it also owns the AppHeader);
// this page still needs the current user itself, to tell the activity log's
// "you" apart from other authors — getCurrentUser is cache()'d, so this
// doesn't add a request beyond what the layout already made.
export default async function TaskPage({
  params,
}: {
  params: Promise<{ projectId: string; taskId: string }>;
}) {
  const { projectId, taskId } = await params;
  const [task, project, user] = await Promise.all([
    getTask(taskId),
    getProject(projectId),
    getCurrentUser(),
  ]);
  if (!task || !project || !user) notFound();
  // A task reached through the wrong project's URL is not that project's task,
  // so the nested route treats it as missing rather than rendering it here.
  if (task.projectId !== projectId) notFound();

  // The dependency pickers choose from the project's other tasks, so the
  // single view loads the collection alongside the task itself.
  const [backlogs, tasks, dependencies, linkedGitlabProjects, comments] = await Promise.all([
    getBacklogs(projectId),
    getTasks(projectId),
    getTaskDependencies(projectId),
    getLinkedGitlabProjects(projectId),
    getTaskComments(taskId),
  ]);

  // Assignee/labels are edited against a specific linked GitLab project's
  // candidates (issue #80). A project with no GitLab connection, or none
  // linked yet, has no candidates to offer — the edit form falls back to
  // free-text entry for both fields in that case.
  const defaultLink = linkedGitlabProjects.find((l) => l.isDefault) ?? null;
  let assigneeOptions: GitlabMemberOption[] | null = null;
  let labelOptions: GitlabLabelOption[] | null = null;
  if (defaultLink) {
    [assigneeOptions, labelOptions] = await Promise.all([
      getLinkedGitlabProjectMembers(defaultLink.id),
      getLinkedGitlabProjectLabels(defaultLink.id),
    ]);
  }

  // Naming an agent comment's author needs the project's token roster, which
  // is owner-only (issue #100) — left empty for anyone else, the same
  // fallback the Project single view uses for the same endpoint.
  let apiTokens: ApiToken[] = [];
  try {
    apiTokens = await getProjectApiTokens(projectId);
  } catch {
    // Left empty; the activity log still renders, agent comments just show
    // without a token name.
  }

  // The merge requests that reference this task (issue #112's reverse
  // link) — fetched through the same project-scoped endpoint the
  // MergeRequest collection uses, filtered to this one task.
  let taskMergeRequests: Awaited<ReturnType<typeof getMergeRequests>> = [];
  try {
    taskMergeRequests = await getMergeRequests(projectId, { taskId });
  } catch {
    // Left empty; the rest of the task single view still renders.
  }

  return (
    <>
      <Breadcrumbs
        items={[{ label: "Tasks", href: tasksPath(project.id) }, { label: task.title }]}
      />
      <TaskDetail
        task={task}
        backlogs={backlogs}
        tasks={tasks}
        dependencies={dependencies}
        assigneeOptions={assigneeOptions}
        labelOptions={labelOptions}
        comments={comments}
        currentUserId={user.id}
        apiTokens={apiTokens}
        mergeRequests={taskMergeRequests}
      />
    </>
  );
}
