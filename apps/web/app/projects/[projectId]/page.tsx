import { redirect, notFound } from "next/navigation";
import { getCurrentUser, getProject } from "@/lib/api";
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

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <ProjectDetail project={project} />
      </main>
    </>
  );
}
