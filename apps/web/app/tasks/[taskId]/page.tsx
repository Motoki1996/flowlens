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
  // This route isn't nested under any layout that already checks auth, and
  // middleware.ts only checks that the session cookie exists (not that it's
  // still valid) — without this, an expired cookie would reach getTask()
  // below and surface as an unhandled fetch error instead of a clean
  // redirect to /login.
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { taskId } = await params;
  const task = await getTask(taskId);
  if (!task) notFound();

  redirect(taskPath(task.projectId, task.id));
}
