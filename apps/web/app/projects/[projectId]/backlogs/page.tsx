import { notFound } from "next/navigation";
import { getBacklogs, getProject } from "@/lib/api";
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

  // taskCount/closedTaskCount are aggregated server-side onto each backlog
  // (issue #144), so the collection no longer needs every task in the
  // project just to show a count and a completion ratio.
  const backlogs = await getBacklogs(projectId);

  return <BacklogListSection projectId={project.id} backlogs={backlogs} />;
}
