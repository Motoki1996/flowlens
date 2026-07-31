import { redirect, notFound } from "next/navigation";
import { getCurrentUser, getTask } from "@/lib/api";
import { taskPath } from "@/lib/routes";

/**
 * The task single view moved under its project (/projects/[projectId]/tasks/[taskId])
 * so the collection and single routes mirror each other. This route stays
 * behind to forward links that were made — or bookmarked — before the move.
 */
export default async function LegacyTaskPage({
  params,
}: {
  params: Promise<{ taskId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { taskId } = await params;
  const task = await getTask(taskId);
  if (!task) notFound();

  redirect(taskPath(task.projectId, task.id));
}
