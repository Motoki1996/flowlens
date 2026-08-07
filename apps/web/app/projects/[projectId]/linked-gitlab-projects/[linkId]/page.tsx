import { notFound } from "next/navigation";
import type { SyncRun, WebhookEventPage } from "@/types";
import { getLinkedGitlabProject, getProject, getSyncRuns, getWebhookEvents } from "@/lib/api";
import { gitlabConnectionPath } from "@/lib/routes";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { LinkedGitlabProjectDetail } from "@/components/LinkedGitlabProjectDetail";

// Auth is guarded by the parent layout.tsx (it also owns the AppHeader this
// page doesn't need its own user object for).
export default async function LinkedGitlabProjectPage({
  params,
}: {
  params: Promise<{ projectId: string; linkId: string }>;
}) {
  const { projectId, linkId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  // The read is project-scoped by the API itself, so a link belonging to
  // another project (or another user) arrives here as a plain 404, the same
  // as the task and backlog single views.
  const link = await getLinkedGitlabProject(projectId, linkId);
  if (!link) notFound();

  let syncRuns: SyncRun[] = [];
  try {
    syncRuns = await getSyncRuns(link.id);
  } catch {
    // Left empty; the history renders as "no sync runs" and "Sync now" still
    // works.
  }

  let webhookEvents: WebhookEventPage = { events: [], nextPage: 0 };
  try {
    webhookEvents = await getWebhookEvents(link.id);
  } catch {
    // Left empty; the screen still renders with no recent events.
  }

  return (
    <>
      <Breadcrumbs
        items={[
          { label: "GitLab connection", href: gitlabConnectionPath(project.id) },
          { label: link.pathWithNamespace },
        ]}
      />
      <LinkedGitlabProjectDetail
        projectId={project.id}
        link={link}
        syncRuns={syncRuns}
        webhookEvents={webhookEvents.events}
        webhookEventsNextPage={webhookEvents.nextPage}
      />
    </>
  );
}
