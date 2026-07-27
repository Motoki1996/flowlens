import { redirect, notFound } from "next/navigation";
import {
  getBacklogs,
  getCurrentUser,
  getGitlabConnection,
  getLinkedGitlabProjects,
  getProject,
  getTasks,
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

  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let tasksError = false;
  try {
    [tasks, backlogs] = await Promise.all([getTasks(projectId), getBacklogs(projectId)]);
  } catch {
    tasksError = true;
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

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <ProjectDetail
          project={project}
          tasks={tasks}
          backlogs={backlogs}
          tasksError={tasksError}
          gitlabConnection={gitlabConnection}
          linkedGitlabProjects={linkedGitlabProjects}
        />
      </main>
    </>
  );
}
