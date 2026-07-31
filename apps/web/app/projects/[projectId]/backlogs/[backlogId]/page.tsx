import { redirect, notFound } from "next/navigation";
import { getBacklog, getCurrentUser, getProject, getTasks } from "@/lib/api";
import { backlogsPath, projectPath } from "@/lib/routes";
import { AppHeader } from "@/components/AppHeader";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { BacklogDetail } from "@/components/BacklogDetail";
import type { Task } from "@/types";

export default async function BacklogPage({
  params,
}: {
  params: Promise<{ projectId: string; backlogId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

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
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Breadcrumbs
          items={[
            { label: project.name, href: projectPath(project.id) },
            { label: "Backlogs", href: backlogsPath(project.id) },
            { label: backlog.name },
          ]}
        />
        <BacklogDetail backlog={backlog} project={project} tasks={tasks} tasksError={tasksError} />
      </main>
    </>
  );
}
