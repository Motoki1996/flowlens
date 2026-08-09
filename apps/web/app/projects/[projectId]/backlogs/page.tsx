import { notFound } from "next/navigation";
import { getBacklogs, getProject, getTasks } from "@/lib/api";
import { BacklogListSection } from "@/components/BacklogListSection";

/** The Backlog collection view of one project. Auth is guarded by the parent
 *  layout.tsx. */
export default async function BacklogsPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  const backlogs = await getBacklogs(projectId);

  // Tasks feed the per-backlog count and the Timeline view's completion bars,
  // so a failure leaves the collection rendering and is reported there rather
  // than failing the whole screen.
  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  let tasksError = false;
  try {
    tasks = await getTasks(projectId);
  } catch {
    tasksError = true;
  }

  return (
    <BacklogListSection
      projectId={project.id}
      backlogs={backlogs}
      tasks={tasks}
      tasksError={tasksError}
    />
  );
}
