import { notFound } from "next/navigation";
import {
  getBacklog,
  getEpics,
  getLinkedGitlabProjects,
  getProject,
  getTasks,
} from "@/lib/api";
import { backlogsPath } from "@/lib/routes";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { BacklogDetail } from "@/components/BacklogDetail";
import type { LinkedGitlabProject, Task } from "@/types";

// Auth is guarded by the parent layout.tsx (it also owns the AppHeader this
// page doesn't need its own user object for).
export default async function BacklogPage({
  params,
}: {
  params: Promise<{ projectId: string; backlogId: string }>;
}) {
  const { projectId, backlogId } = await params;
  const [backlog, project] = await Promise.all([getBacklog(backlogId), getProject(projectId)]);
  if (!backlog || !project) notFound();
  // A backlog reached through the wrong project's URL is not that project's
  // backlog, so the nested route treats it as missing.
  if (backlog.projectId !== projectId) notFound();

  // The epics filed in this backlog, for the single view's Epics card. An
  // empty list — a backlog broken straight down into tasks — hides the card,
  // and a failed fetch reads the same way rather than taking the screen down.
  let epics: Awaited<ReturnType<typeof getEpics>> = [];
  try {
    epics = await getEpics(projectId, { backlogId: backlog.id });
  } catch {
    epics = [];
  }

  let tasks: Task[] = [];
  let tasksError = false;
  try {
    const projectTasks = await getTasks(projectId);
    tasks = projectTasks.filter((t) => t.backlogId === backlog.id);
  } catch {
    tasksError = true;
  }

  // Names this backlog's issue destination (issue #180). A project with no
  // GitLab connection has none, which reads the same as a failed fetch: the
  // row simply doesn't appear.
  let links: LinkedGitlabProject[] = [];
  try {
    links = await getLinkedGitlabProjects(projectId);
  } catch {
    links = [];
  }

  return (
    <>
      <Breadcrumbs
        items={[
          { label: "Backlogs", href: backlogsPath(project.id) },
          { label: backlog.name },
        ]}
      />
      <BacklogDetail
        backlog={backlog}
        epics={epics}
        project={project}
        tasks={tasks}
        links={links}
        tasksError={tasksError}
      />
    </>
  );
}
