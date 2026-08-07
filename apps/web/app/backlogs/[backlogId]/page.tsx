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
  // This route isn't nested under any layout that already checks auth, and
  // middleware.ts only checks that the session cookie exists (not that it's
  // still valid) — without this, an expired cookie would reach getBacklog()
  // below and surface as an unhandled fetch error instead of a clean
  // redirect to /login.
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { backlogId } = await params;
  const backlog = await getBacklog(backlogId);
  if (!backlog) notFound();

  redirect(backlogPath(backlog.projectId, backlog.id));
}
