import { redirect, notFound } from "next/navigation";
import { getBacklogs, getCurrentUser, getProject, getTaskDependencies, getTasks } from "@/lib/api";
import { TaskListSection } from "@/components/TaskListSection";

/**
 * The Task collection view of one project — List and Timeline are view modes
 * of this one screen, per docs/ui-design.md rule 5, and `?backlog=` is the
 * backlog filter of that same collection. The backlog screens link here rather
 * than keeping a task list of their own.
 */
export default async function TasksPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{
    backlog?: string;
    q?: string;
    status?: string;
    progress?: string;
    sort?: string;
  }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;
  const backlogFilter = resolvedSearchParams?.backlog;
  const search = resolvedSearchParams?.q;
  const statusFilter = resolvedSearchParams?.status;
  const progressFilter = resolvedSearchParams?.progress;
  const sort = resolvedSearchParams?.sort;
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
    <TaskListSection
      projectId={project.id}
      tasks={tasks}
      backlogs={backlogs}
      dependencies={taskDependencies}
      initialBacklogFilter={backlogFilter}
      initialSearch={search}
      initialStatusFilter={statusFilter}
      initialProgressFilter={progressFilter}
      initialSort={sort}
      error={tasksError}
    />
  );
}
