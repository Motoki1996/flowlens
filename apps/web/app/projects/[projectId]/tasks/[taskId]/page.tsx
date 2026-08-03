import { redirect, notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getLinkedGitlabProjectLabels,
  getLinkedGitlabProjectMembers,
  getLinkedGitlabProjects,
  getProject,
  getTask,
  getTaskDependencies,
  getTasks,
} from "@/lib/api";
import { tasksPath } from "@/lib/routes";
import type { GitlabLabelOption, GitlabMemberOption } from "@/types";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { TaskDetail } from "@/components/TaskDetail";

export default async function TaskPage({
  params,
}: {
  params: Promise<{ projectId: string; taskId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { projectId, taskId } = await params;
  const [task, project] = await Promise.all([getTask(taskId), getProject(projectId)]);
  if (!task || !project) notFound();
  // A task reached through the wrong project's URL is not that project's task,
  // so the nested route treats it as missing rather than rendering it here.
  if (task.projectId !== projectId) notFound();

  // The dependency pickers choose from the project's other tasks, so the
  // single view loads the collection alongside the task itself.
  const [backlogs, tasks, dependencies, linkedGitlabProjects] = await Promise.all([
    getBacklogs(projectId),
    getTasks(projectId),
    getTaskDependencies(projectId),
    getLinkedGitlabProjects(projectId),
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
      />
    </>
  );
}
