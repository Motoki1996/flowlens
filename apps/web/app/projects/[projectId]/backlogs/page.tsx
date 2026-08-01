import { redirect, notFound } from "next/navigation";
import { getBacklogs, getCurrentUser, getProject, getTasks } from "@/lib/api";
import { BacklogListSection } from "@/components/BacklogListSection";

/** The Backlog collection view of one project. */
export default async function BacklogsPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { projectId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  const backlogs = await getBacklogs(projectId);

  // Tasks are only here for the per-backlog count, so a failure leaves the
  // list rendering with zeroes rather than failing the whole screen.
  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  try {
    tasks = await getTasks(projectId);
  } catch {
    // Counts fall back to 0.
  }

  return <BacklogListSection projectId={project.id} backlogs={backlogs} tasks={tasks} />;
}
