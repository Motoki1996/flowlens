import { redirect } from "next/navigation";
import { getCurrentUser, getProjects } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { ProjectsView } from "@/components/ProjectsView";
import type { Project } from "@/types";

export default async function ProjectsPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  let projects: Project[] = [];
  let loadError = false;
  try {
    projects = await getProjects();
  } catch {
    loadError = true;
  }

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <ProjectsView projects={projects} error={loadError} />
      </main>
    </>
  );
}
