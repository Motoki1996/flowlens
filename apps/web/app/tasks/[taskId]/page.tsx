import { redirect, notFound } from "next/navigation";
import { getBacklogs, getCurrentUser, getProject, getTask } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { TaskDetail } from "@/components/TaskDetail";

export default async function TaskPage({
  params,
}: {
  params: Promise<{ taskId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { taskId } = await params;
  const task = await getTask(taskId);
  if (!task) notFound();

  const project = await getProject(task.projectId);
  if (!project) notFound();

  const backlogs = await getBacklogs(task.projectId);

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <TaskDetail task={task} project={project} backlogs={backlogs} />
      </main>
    </>
  );
}
