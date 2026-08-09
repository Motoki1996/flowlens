import { notFound } from "next/navigation";
import { getGitlabConnection, getLinkedGitlabProjects, getProject } from "@/lib/api";
import { GitlabConnectionDetail } from "@/components/GitlabConnectionDetail";
import { LinkedGitlabProjectListSection } from "@/components/LinkedGitlabProjectListSection";

/**
 * The GitLab connection single view, with the LinkedGitlabProject collection
 * it owns below it. Auth is guarded by the parent layout.tsx.
 */
export default async function GitlabConnectionPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  let connection: Awaited<ReturnType<typeof getGitlabConnection>> = null;
  let links: Awaited<ReturnType<typeof getLinkedGitlabProjects>> = [];
  try {
    [connection, links] = await Promise.all([
      getGitlabConnection(projectId),
      getLinkedGitlabProjects(projectId),
    ]);
  } catch {
    // Left as their defaults (disconnected, no links); the screen still
    // renders and lets the user retry via its own actions.
  }

  return (
    <>
      <GitlabConnectionDetail projectId={project.id} connection={connection} />
      <div className="mt-8">
        <LinkedGitlabProjectListSection
          projectId={project.id}
          links={links}
          connected={connection !== null}
        />
      </div>
    </>
  );
}
