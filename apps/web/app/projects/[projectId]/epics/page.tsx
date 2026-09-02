import { notFound } from "next/navigation";
import {
  getBacklogs,
  getEpics,
  getLinkedGitlabProjects,
  getProject,
  getProjectMembers,
  getTasks,
  MAX_TASKS_PER_PAGE,
} from "@/lib/api";
import { EpicListSection, NO_BACKLOG_FILTER } from "@/components/EpicListSection";
import type { ViewMode } from "@/components/ViewModeToggle";
import type { Priority, Progress, StatusFilter, Task } from "@/types";

// The ?status= values the collection accepts; "open" is the default and drops
// out of the query string, the same as the Backlog collection's.
const STATUSES = ["open", "closed", "all"] as const;
const PROGRESSES = ["not_started", "in_progress", "on_hold", "done"] as const;
const PRIORITIES = ["low", "medium", "high", "urgent"] as const;

// "manual" is the API's own default (creation) order, expressed by the absence of
// ?sort= rather than a named value, the same as the Backlog and Task
// collections. "dueOn" is the one value the API doesn't accept — it's applied
// client-side instead, so it's a recognised URL value that never reaches
// getEpics below.
const NAMED_SORTS = ["dueOn", "priority", "progress"] as const;
type NamedSort = (typeof NAMED_SORTS)[number];

const VIEW_MODES = ["board", "list", "timeline"] as const;

/**
 * The Epic collection view of one project — Board (the default), List and
 * Timeline are view modes of this one screen, per docs/ui-design.md rule 5.
 * `?backlog=`/`?priority=`/`?progress=`/`?sort=` mirror the Backlog
 * collection's own filters and, like them, live in the URL and are applied by
 * the API rather than the client. An unrecognised value falls back to that
 * filter's default rather than erroring, since these come straight from a
 * hand-editable query string.
 *
 * Auth is guarded by the parent layout.tsx.
 */
export default async function EpicsPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{
    status?: string;
    backlog?: string;
    priority?: string;
    progress?: string;
    sort?: string;
    view?: string;
  }>;
}) {
  const { projectId } = await params;
  const project = await getProject(projectId);
  if (!project) notFound();

  const resolvedSearchParams = await searchParams;
  const statusParam = resolvedSearchParams?.status;
  const status: StatusFilter = STATUSES.includes(statusParam as StatusFilter)
    ? (statusParam as StatusFilter)
    : "open";
  const backlogFilter = resolvedSearchParams?.backlog;
  const priorityParam = resolvedSearchParams?.priority;
  const priority = PRIORITIES.includes(priorityParam as Priority)
    ? (priorityParam as Priority)
    : undefined;
  const progressParam = resolvedSearchParams?.progress;
  const progress = PROGRESSES.includes(progressParam as Progress)
    ? (progressParam as Progress)
    : undefined;
  const sortParam = resolvedSearchParams?.sort;
  const sort = NAMED_SORTS.includes(sortParam as NamedSort)
    ? (sortParam as NamedSort)
    : ("manual" as const);
  const viewParam = resolvedSearchParams?.view;
  const initialView: ViewMode = VIEW_MODES.includes(viewParam as ViewMode)
    ? (viewParam as ViewMode)
    : "board";

  // getProject above is left unguarded: without a project there's nothing on
  // this screen to fall back to, so its failure bubbles up to error.tsx, the
  // same as every other project screen. getEpics is caught here instead,
  // mirroring the Backlog collection, so a DB hiccup on just this fetch
  // doesn't take the "New epic" action and the rest of the screen down too.
  let epics: Awaited<ReturnType<typeof getEpics>> = [];
  let epicsError = false;
  try {
    epics = await getEpics(projectId, {
      status,
      // The URL says "none"; the API spells the same thing "unassigned" on
      // backlog_id, the value the task collection already uses.
      backlogId: backlogFilter === NO_BACKLOG_FILTER ? "unassigned" : backlogFilter,
      priority,
      progress,
      sort: sort === "priority" || sort === "progress" ? sort : undefined,
    });
  } catch {
    epicsError = true;
  }

  // The backlogs an epic can be filed in, and the linked GitLab projects it
  // can name as its own issue destination. Both are caught separately from
  // the epics above: a project with no GitLab connection has no links, and
  // that empty list is the same outcome as a failed fetch — the forms simply
  // drop the field rather than taking the screen down.
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  try {
    // "all", not the default: these backlogs are a lookup table (the filter
    // row's names, the create/edit form's parent picker), and a closed backlog
    // still has epics in it whose row would otherwise say "No backlog".
    backlogs = await getBacklogs(projectId, { status: "all" });
  } catch {
    backlogs = [];
  }

  // The create/edit form's task picker draws from the project's tasks. A
  // failed fetch drops the field rather than the screen, the same as links
  // below.
  // A picker, not a browsable list — so it asks for the largest page the API
  // will give rather than paging: a project past MAX_TASKS_PER_PAGE tasks
  // shows the first page of candidates instead of an unbounded response.
  let tasks: Task[] = [];
  try {
    tasks = (await getTasks(projectId, { perPage: MAX_TASKS_PER_PAGE })).tasks;
  } catch {
    tasks = [];
  }

  let links: Awaited<ReturnType<typeof getLinkedGitlabProjects>> = [];
  try {
    links = await getLinkedGitlabProjects(projectId);
  } catch {
    links = [];
  }

  // The create/edit form's assignee picker. A failed fetch drops the field
  // rather than the screen, the same as links above.
  let members: Awaited<ReturnType<typeof getProjectMembers>> = null;
  try {
    members = await getProjectMembers(projectId);
  } catch {
    members = null;
  }

  return (
    <EpicListSection
      projectId={project.id}
      epics={epics}
      backlogs={backlogs}
      tasks={tasks}
      links={links}
      members={members}
      statusFilter={status}
      backlogFilter={backlogFilter}
      priorityFilter={priority}
      progressFilter={progress}
      sort={sort}
      initialView={initialView}
      error={epicsError}
    />
  );
}
