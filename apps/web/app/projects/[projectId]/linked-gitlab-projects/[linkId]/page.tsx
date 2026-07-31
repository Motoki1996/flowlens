import { redirect, notFound } from "next/navigation";
import type { SyncRun, WebhookEvent } from "@/types";
import {
  getCurrentUser,
  getLinkedGitlabProjects,
  getProject,
  getSyncRuns,
  getWebhookEvents,
} from "@/lib/api";
import { gitlabConnectionPath, projectPath } from "@/lib/routes";
import { AppHeader } from "@/components/AppHeader";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { LinkedGitlabProjectDetail } from "@/components/LinkedGitlabProjectDetail";

export default async function LinkedGitlabProjectPage({
  params,
}: {
  params: Promise<{ projectId: string; linkId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const { projectId, linkId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  // The API lists a project's links but has no single-link endpoint, so the
  // link is picked out of the project's own list — which also scopes it to
  // this project for free.
  const links = await getLinkedGitlabProjects(projectId);
  const link = links.find((l) => l.id === linkId);
  if (!link) notFound();

  let syncRuns: SyncRun[] = [];
  try {
    syncRuns = await getSyncRuns(link.id);
  } catch {
    // Left empty; the history renders as "no sync runs" and "Sync now" still
    // works.
  }

  let webhookEvents: WebhookEvent[] = [];
  try {
    webhookEvents = await getWebhookEvents(link.id);
  } catch {
    // Left empty; the screen still renders with no recent events.
  }

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Breadcrumbs
          items={[
            { label: project.name, href: projectPath(project.id) },
            { label: "GitLab connection", href: gitlabConnectionPath(project.id) },
            { label: link.pathWithNamespace },
          ]}
        />
        <LinkedGitlabProjectDetail
          projectId={project.id}
          link={link}
          syncRuns={syncRuns}
          webhookEvents={webhookEvents}
        />
      </main>
    </>
  );
}
