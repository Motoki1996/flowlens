import { notFound } from "next/navigation";
import { getBacklog, getProject, getTasks } from "@/lib/api";
import { backlogsPath } from "@/lib/routes";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { BacklogDetail } from "@/components/BacklogDetail";
import type { Task } from "@/types";

// Auth is guarded by the parent layout.tsx (it also owns the AppHeader this
// page doesn't need its own user object for).
export default async function BacklogPage({
  params,
}: {
  params: Promise<{ projectId: string; backlogId: string }>;
}) {
  const { projectId, backlogId } = await params;
  const [backlog, project] = await Promise.all([getBacklog(backlogId), getProject(projectId)]);
  if (!backlog || !project) notFound();
  // A backlog reached through the wrong project's URL is not that project's
  // backlog, so the nested route treats it as missing.
  if (backlog.projectId !== projectId) notFound();

  let tasks: Task[] = [];
  let tasksError = false;
  try {
    const projectTasks = await getTasks(projectId);
    tasks = projectTasks.filter((t) => t.backlogId === backlog.id);
  } catch {
    tasksError = true;
  }

  return (
    <>
      <Breadcrumbs
        items={[
          { label: "Backlogs", href: backlogsPath(project.id) },
          { label: backlog.name },
        ]}
      />
      <BacklogDetail backlog={backlog} project={project} tasks={tasks} tasksError={tasksError} />
    </>
  );
}
