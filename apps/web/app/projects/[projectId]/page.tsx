import { redirect, notFound } from "next/navigation";
import type { SyncRun, WebhookEvent } from "@/types";
import {
  getBacklogs,
  getCurrentUser,
  getGitlabConnection,
  getLinkedGitlabProjects,
  getProject,
  getSyncRuns,
  getTasks,
  getWebhookEvents,
} from "@/lib/api";
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

  // Tasks and backlogs have screens of their own; this view only needs enough
  // of them to show a count next to each link.
  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let countsError = false;
  try {
    [tasks, backlogs] = await Promise.all([getTasks(projectId), getBacklogs(projectId)]);
  } catch {
    countsError = true;
  }

  let gitlabConnection: Awaited<ReturnType<typeof getGitlabConnection>> = null;
  let linkedGitlabProjects: Awaited<ReturnType<typeof getLinkedGitlabProjects>> = [];
  try {
    [gitlabConnection, linkedGitlabProjects] = await Promise.all([
      getGitlabConnection(projectId),
      getLinkedGitlabProjects(projectId),
    ]);
  } catch {
    // Left as their defaults (disconnected, no links); the section still
    // renders and lets the user retry via its own actions.
  }

  let syncRunsByLink: Record<string, SyncRun[]> = {};
  try {
    const entries = await Promise.all(
      linkedGitlabProjects.map(async (link) => [link.id, await getSyncRuns(link.id)] as const),
    );
    syncRunsByLink = Object.fromEntries(entries);
  } catch {
    // Left empty; the section still renders with no history and lets the
    // user retry via "Sync now".
  }

  let webhookEventsByLink: Record<string, WebhookEvent[]> = {};
  try {
    const entries = await Promise.all(
      linkedGitlabProjects.map(async (link) => [link.id, await getWebhookEvents(link.id)] as const),
    );
    webhookEventsByLink = Object.fromEntries(entries);
  } catch {
    // Left empty; the section still renders with no recent events.
  }

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <ProjectDetail
          project={project}
          backlogCount={backlogs.length}
          taskCount={tasks.length}
          openTaskCount={tasks.filter((t) => t.status === "open").length}
          countsError={countsError}
          gitlabConnection={gitlabConnection}
          linkedGitlabProjects={linkedGitlabProjects}
          syncRunsByLink={syncRunsByLink}
          webhookEventsByLink={webhookEventsByLink}
        />
      </main>
    </>
  );
}
