import { redirect, notFound } from "next/navigation";
import { getBacklog, getCurrentUser } from "@/lib/api";
import { backlogPath } from "@/lib/routes";

/**
 * The backlog single view moved under its project
 * (/projects/[projectId]/backlogs/[backlogId]); this route forwards links made
 * before the move.
 */
export default async function LegacyBacklogPage({
  params,
}: {
  params: Promise<{ backlogId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { backlogId } = await params;
  const backlog = await getBacklog(backlogId);
  if (!backlog) notFound();

  redirect(backlogPath(backlog.projectId, backlog.id));
}
