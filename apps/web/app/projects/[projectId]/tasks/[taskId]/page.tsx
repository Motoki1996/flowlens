import { notFound } from "next/navigation";
import {
  getBacklogs,
  getEpics,
  getCurrentUser,
  getLinkedGitlabProjectLabels,
  getLinkedGitlabProjectMembers,
  getLinkedGitlabProjects,
  getMergeRequests,
  getProject,
  getProjectApiTokens,
  getProjectMembers,
  getTask,
  getTaskComments,
  getTaskDependencies,
  getTasks,
  MAX_TASKS_PER_PAGE,
} from "@/lib/api";
import { tasksPath } from "@/lib/routes";
import type { ApiToken, GitlabLabelOption, GitlabMemberOption, MergeRequest } from "@/types";
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
  const [backlogs, epics, tasks, dependencies, linkedGitlabProjects, comments] =
    await Promise.all([
      // "all", for the same reason the collection uses it: this task may sit
      // in a closed backlog or epic, and both the label and the edit form's
      // picker have to be able to name it.
      getBacklogs(projectId, { status: "all" }),
      getEpics(projectId, { status: "all" }),
      // A picker for the dependency fields, not a browsable list: the largest
      // page the API gives rather than the project's tasks without bound.
      getTasks(projectId, { perPage: MAX_TASKS_PER_PAGE }),
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

  // The edit form's FlowLens assignee picker. A failed fetch drops the field
  // rather than the screen, the same as assigneeOptions/labelOptions above.
  let members: Awaited<ReturnType<typeof getProjectMembers>> = null;
  try {
    members = await getProjectMembers(projectId);
  } catch {
    members = null;
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
  let taskMergeRequests: MergeRequest[] = [];
  try {
    // A task has at most a handful of merge requests, so its own card wants
    // the first page and never pages further.
    taskMergeRequests = (await getMergeRequests(projectId, { taskId })).mergeRequests;
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
        epics={epics}
        tasks={tasks.tasks}
        dependencies={dependencies}
        assigneeOptions={assigneeOptions}
        labelOptions={labelOptions}
        members={members}
        comments={comments}
        currentUserId={user.id}
        apiTokens={apiTokens}
        mergeRequests={taskMergeRequests}
      />
    </>
  );
}
