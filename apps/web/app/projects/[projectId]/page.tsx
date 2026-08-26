import { notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getEpics,
  getFailedSyncJobs,
  getGitlabConnection,
  getLinkedGitlabProjects,
  getProject,
  getProjectApiTokens,
  getProjectFlowMetrics,
  getProjectInvites,
  getProjectMembers,
  getProjectMetrics,
  getProjectVelocity,
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
  searchParams?: Promise<{ from?: string; to?: string; interval?: string }>;
}) {
  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;
  const metricsFrom = resolvedSearchParams?.from;
  const metricsTo = resolvedSearchParams?.to;
  const metricsInterval =
    resolvedSearchParams?.interval === "week" ||
    resolvedSearchParams?.interval === "month" ||
    resolvedSearchParams?.interval === "year"
      ? resolvedSearchParams.interval
      : undefined;

  const project = await getProject(projectId);
  if (!project) notFound();

  // Auth itself is the layout's job; this read (memoised per request) only
  // tells the members section which row is the viewer's own.
  const currentUser = await getCurrentUser();

  // Tasks, backlogs and epics have screens of their own; this view only needs
  // enough of them to show a count next to each link.
  // perPage: 1 — this view only shows the two task counts, and both are
  // counted in SQL, so there is nothing to gain by fetching the rows.
  let tasks: Awaited<ReturnType<typeof getTasks>> = {
    tasks: [],
    nextPage: 0,
    totalCount: 0,
    openCount: 0,
  };
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let epics: Awaited<ReturnType<typeof getEpics>> = [];
  let countsError = false;
  try {
    [tasks, backlogs, epics] = await Promise.all([
      getTasks(projectId, { perPage: 1 }),
      getBacklogs(projectId),
      getEpics(projectId),
    ]);
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

  // null (not []) is the failure default here for the same reason as
  // members above: the invites listing is owner-only, and its card is
  // hidden rather than shown empty for anyone else.
  let invites: Awaited<ReturnType<typeof getProjectInvites>> = null;
  try {
    invites = await getProjectInvites(projectId);
  } catch {
    // Left null; the card is simply not rendered.
  }

  // Delivery metrics (issue #113) and flow metrics (issue #171) share the
  // same date-range filter and render in the same card, so a failure on
  // either side falls back to the same error state.
  let metrics: Awaited<ReturnType<typeof getProjectMetrics>> | null = null;
  let flowMetrics: Awaited<ReturnType<typeof getProjectFlowMetrics>> | null = null;
  let metricsError = false;
  try {
    [metrics, flowMetrics] = await Promise.all([
      getProjectMetrics(projectId, { from: metricsFrom, to: metricsTo, interval: metricsInterval }),
      getProjectFlowMetrics(projectId, { from: metricsFrom, to: metricsTo, interval: metricsInterval }),
    ]);
  } catch {
    metricsError = true;
  }

  // Velocity (issue #195) shares the same [from, to, interval] URL filter as
  // the delivery/flow metrics above (issue #196), but is fetched and can
  // fail independently — it's a separate API endpoint with its own
  // authorization check.
  let velocity: Awaited<ReturnType<typeof getProjectVelocity>> | null = null;
  let velocityError = false;
  try {
    velocity = await getProjectVelocity(projectId, { from: metricsFrom, to: metricsTo, interval: metricsInterval });
  } catch {
    velocityError = true;
  }

  return (
    <ProjectDetail
      project={project}
      backlogCount={backlogs.length}
      epicCount={epics.length}
      taskCount={tasks.totalCount}
      openTaskCount={tasks.openCount}
      countsError={countsError}
      gitlabConnection={gitlabConnection}
      linkedProjectCount={linkedGitlabProjects.length}
      apiTokens={apiTokens}
      failedSyncJobs={failedSyncJobs}
      members={members}
      invites={invites}
      currentUserId={currentUser?.id ?? ""}
      metrics={metrics}
      flowMetrics={flowMetrics}
      velocity={velocity}
      metricsError={metricsError}
      velocityError={velocityError}
      metricsFrom={metricsFrom}
      metricsTo={metricsTo}
      metricsInterval={metricsInterval}
    />
  );
}
