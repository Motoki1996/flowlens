import { redirect, notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getGitlabConnection,
  getLinkedGitlabProjects,
  getProject,
  getProjectApiTokens,
  getTasks,
} from "@/lib/api";
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
    />
  );
}
