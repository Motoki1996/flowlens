import { redirect, notFound } from "next/navigation";
import { getBacklogs, getCurrentUser, getProject, getTask } from "@/lib/api";
import { tasksPath } from "@/lib/routes";
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

  const backlogs = await getBacklogs(projectId);

  return (
    <>
      <Breadcrumbs
        items={[{ label: "Tasks", href: tasksPath(project.id) }, { label: task.title }]}
      />
      <TaskDetail task={task} backlogs={backlogs} />
    </>
  );
}
