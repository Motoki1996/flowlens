import { redirect, notFound } from "next/navigation";
import { getBacklogs, getCurrentUser, getProject, getTasks } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { ProjectDetail } from "@/components/ProjectDetail";

export default async function ProjectPage({
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

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <ProjectDetail project={project} tasks={tasks} backlogs={backlogs} tasksError={tasksError} />
      </main>
    </>
  );
}
