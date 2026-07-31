import { redirect, notFound } from "next/navigation";
import { getBacklogs, getCurrentUser, getProject, getTaskDependencies, getTasks } from "@/lib/api";
import { projectPath } from "@/lib/routes";
import { AppHeader } from "@/components/AppHeader";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { TaskListSection } from "@/components/TaskListSection";

/** The Task collection view of one project — List and Timeline are view modes
 *  of this one screen, per docs/ui-design.md rule 5. */
export default async function TasksPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { projectId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let tasksError = false;
  try {
    [tasks, backlogs] = await Promise.all([getTasks(projectId), getBacklogs(projectId)]);
  } catch {
    tasksError = true;
  }

  let taskDependencies: Awaited<ReturnType<typeof getTaskDependencies>> = [];
  try {
    taskDependencies = await getTaskDependencies(projectId);
  } catch {
    // Left empty; the timeline view still renders tasks, just without
    // dependency lines.
  }

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Breadcrumbs
          items={[
            { label: project.name, href: projectPath(project.id) },
            { label: "Tasks" },
          ]}
        />
        <TaskListSection
          projectId={project.id}
          tasks={tasks}
          backlogs={backlogs}
          dependencies={taskDependencies}
          error={tasksError}
        />
      </main>
    </>
  );
}
