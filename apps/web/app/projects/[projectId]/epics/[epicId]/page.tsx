import { notFound } from "next/navigation";
import {
  getBacklogs,
  getEpic,
  getLinkedGitlabProjects,
  getProject,
  getTasks,
} from "@/lib/api";
import { backlogPath, backlogsPath, epicsPath } from "@/lib/routes";
import { Breadcrumbs } from "@/components/Breadcrumbs";
import { EpicDetail } from "@/components/EpicDetail";
import type { Backlog, LinkedGitlabProject, Task } from "@/types";

// Auth is guarded by the parent layout.tsx (it also owns the AppHeader this
// page doesn't need its own user object for).
export default async function EpicPage({
  params,
}: {
  params: Promise<{ projectId: string; epicId: string }>;
}) {
  const { projectId, epicId } = await params;
  const [epic, project] = await Promise.all([getEpic(epicId), getProject(projectId)]);
  if (!epic || !project) notFound();
  // An epic reached through the wrong project's URL is not that project's
  // epic, so the nested route treats it as missing — the same rule the
  // Backlog single view follows.
  if (epic.projectId !== projectId) notFound();

  let tasks: Task[] = [];
  let tasksError = false;
  try {
    tasks = await getTasks(projectId, { epicId: epic.id });
  } catch {
    tasksError = true;
  }

  // The epic's own backlog is what its base branch and scope fall through to
  // when it sets none, so the single view needs the whole backlog, not just
  // its name. Fetched as part of the project's list, which the edit form's
  // parent picker needs anyway.
  let backlogs: Backlog[] = [];
  try {
    backlogs = await getBacklogs(projectId);
  } catch {
    backlogs = [];
  }
  const backlog = epic.backlogId ? (backlogs.find((b) => b.id === epic.backlogId) ?? null) : null;

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
          // The epic's own backlog is the rung above it, so the trail runs
          // through it when there is one — an unfiled epic hangs off the Epic
          // collection directly.
          ...(backlog
            ? [
                { label: "Backlogs", href: backlogsPath(project.id) },
                { label: backlog.name, href: backlogPath(project.id, backlog.id) },
              ]
            : []),
          { label: "Epics", href: epicsPath(project.id) },
          { label: epic.name },
        ]}
      />
      <EpicDetail
        epic={epic}
        project={project}
        backlog={backlog}
        backlogs={backlogs}
        tasks={tasks}
        links={links}
        tasksError={tasksError}
      />
    </>
  );
}
