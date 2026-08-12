import { notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getFailedSyncJobs,
  getGitlabConnection,
  getLinkedGitlabProjects,
  getProject,
  getProjectApiTokens,
  getProjectMembers,
  getProjectMetrics,
  getTasks,
} from "@/lib/api";
import { ProjectDetail } from "@/components/ProjectDetail";

// Auth is guarded by the parent layout.tsx (it also owns the AppHeader this
// page doesn't need its own user object for).
export default async function ProjectPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{ from?: string; to?: string }>;
}) {
  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;
  const metricsFrom = resolvedSearchParams?.from;
  const metricsTo = resolvedSearchParams?.to;

  const project = await getProject(projectId);
  if (!project) notFound();

  // Auth itself is the layout's job; this read (memoised per request) only
  // tells the members section which row is the viewer's own.
  const currentUser = await getCurrentUser();

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

  // Same again for the GitLab connection: this view only reports whether one
  // exists and how many projects it links; managing it happens on its own
  // screen.
  let gitlabConnection: Awaited<ReturnType<typeof getGitlabConnection>> = null;
  let linkedGitlabProjects: Awaited<ReturnType<typeof getLinkedGitlabProjects>> = [];
  try {
    [gitlabConnection, linkedGitlabProjects] = await Promise.all([
      getGitlabConnection(projectId),
      getLinkedGitlabProjects(projectId),
    ]);
  } catch {
    // Left as their defaults (disconnected, no links); the link still leads to
    // the connection screen, which reports the real state.
  }

  let apiTokens: Awaited<ReturnType<typeof getProjectApiTokens>> = [];
  try {
    apiTokens = await getProjectApiTokens(projectId);
  } catch {
    // Left empty; the section still renders and issuing a token still works.
  }

  let failedSyncJobs: Awaited<ReturnType<typeof getFailedSyncJobs>> = [];
  try {
    failedSyncJobs = await getFailedSyncJobs(projectId);
  } catch {
    // Left empty; the section still renders as "no failed sync jobs".
  }

  // null (not []) is the failure default here: getProjectMembers already
  // reports "not an owner" as null, and an unexpected error should render
  // the same read-only state rather than falsely implying an empty project.
  let members: Awaited<ReturnType<typeof getProjectMembers>> = null;
  try {
    members = await getProjectMembers(projectId);
  } catch {
    // Left null; the section renders its read-only state.
  }

  let metrics: Awaited<ReturnType<typeof getProjectMetrics>> | null = null;
  let metricsError = false;
  try {
    metrics = await getProjectMetrics(projectId, { from: metricsFrom, to: metricsTo });
  } catch {
    metricsError = true;
  }

  return (
    <ProjectDetail
      project={project}
      backlogCount={backlogs.length}
      taskCount={tasks.length}
      openTaskCount={tasks.filter((t) => t.status === "open").length}
      countsError={countsError}
      gitlabConnection={gitlabConnection}
      linkedProjectCount={linkedGitlabProjects.length}
      apiTokens={apiTokens}
      failedSyncJobs={failedSyncJobs}
      members={members}
      currentUserId={currentUser?.id ?? ""}
      metrics={metrics}
      metricsError={metricsError}
      metricsFrom={metricsFrom}
      metricsTo={metricsTo}
    />
  );
}
