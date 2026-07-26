import { redirect, notFound } from "next/navigation";
import { getBacklog, getCurrentUser, getProject, getTasks } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { BacklogDetail } from "@/components/BacklogDetail";
import type { Task } from "@/types";

export default async function BacklogPage({
  params,
}: {
  params: Promise<{ backlogId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { backlogId } = await params;
  const backlog = await getBacklog(backlogId);
  if (!backlog) notFound();

  const project = await getProject(backlog.projectId);
  if (!project) notFound();

  let tasks: Task[] = [];
  let tasksError = false;
  try {
    const projectTasks = await getTasks(backlog.projectId);
    tasks = projectTasks.filter((t) => t.backlogId === backlog.id);
  } catch {
    tasksError = true;
  }

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <BacklogDetail backlog={backlog} project={project} tasks={tasks} tasksError={tasksError} />
      </main>
    </>
  );
}
