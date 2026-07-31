import { redirect, notFound } from "next/navigation";
import { getCurrentUser, getGitlabConnection, getLinkedGitlabProjects, getProject } from "@/lib/api";
import { projectPath } from "@/lib/routes";
import { AppHeader } from "@/components/AppHeader";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { GitlabConnectionDetail } from "@/components/GitlabConnectionDetail";
import { LinkedGitlabProjectListSection } from "@/components/LinkedGitlabProjectListSection";

/** The GitLab connection single view, with the LinkedGitlabProject collection
 *  it owns below it. */
export default async function GitlabConnectionPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

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
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Breadcrumbs
          items={[
            { label: project.name, href: projectPath(project.id) },
            { label: "GitLab connection" },
          ]}
        />
        <GitlabConnectionDetail projectId={project.id} connection={connection} />
        <div className="mt-8">
          <LinkedGitlabProjectListSection
            projectId={project.id}
            links={links}
            connected={connection !== null}
          />
        </div>
      </main>
    </>
  );
}
