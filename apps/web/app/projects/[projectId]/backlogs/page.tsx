import { notFound } from "next/navigation";
import {
  getBacklogs,
  getLinkedGitlabProjects,
  getProject,
  getTasks,
} from "@/lib/api";
import { BacklogListSection } from "@/components/BacklogListSection";
import type { ViewMode } from "@/components/ViewModeToggle";
import type { Priority, Progress } from "@/types";

const PROGRESSES = ["not_started", "in_progress", "on_hold", "done"] as const;
const PRIORITIES = ["low", "medium", "high", "urgent"] as const;

// "manual" is the API's own default (creation) order, expressed by the absence of
// ?sort= rather than a named value, same as the Task collection. "dueOn" is
// the one value the API doesn't accept (parseBacklogListFilter,
// internal/http/backlog_handler.go, only takes priority/progress) — it's
// applied client-side instead, so it's still a recognised URL value even
// though it's never forwarded to getBacklogs below.
const NAMED_SORTS = ["dueOn", "priority", "progress"] as const;
type NamedSort = (typeof NAMED_SORTS)[number];
type Sort = "manual" | NamedSort;

// The view modes ViewModeToggle offers, shared by Task and Backlog (issue
// #153) — Board is the default and falls out of the query string entirely.
const VIEW_MODES = ["board", "list", "timeline"] as const;

/**
 * The Backlog collection view of one project — Board (the default), List and
 * Timeline are view modes of this one screen, per docs/ui-design.md rule 5.
 * `?priority=`/`?progress=`/`?sort=` mirror the Task collection's own filters
 * (issue #151) and, like Task's, live in the URL and are applied by the API
 * rather than the client — except `sort=dueOn`, which the API has no concept
 * of and BacklogListSection sorts by itself. An unrecognised value falls back
 * to that filter's default rather than erroring, since these come straight
 * from a hand-editable query string.
 *
 * Auth is guarded by the parent layout.tsx.
 */
export default async function BacklogsPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{
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
  const priorityParam = resolvedSearchParams?.priority;
  const priority = PRIORITIES.includes(priorityParam as Priority)
    ? (priorityParam as Priority)
    : undefined;
  const progressParam = resolvedSearchParams?.progress;
  const progress = PROGRESSES.includes(progressParam as Progress)
    ? (progressParam as Progress)
    : undefined;
  const sortParam = resolvedSearchParams?.sort;
  const sort: Sort = NAMED_SORTS.includes(sortParam as NamedSort)
    ? (sortParam as NamedSort)
    : "manual";
  const viewParam = resolvedSearchParams?.view;
  const initialView: ViewMode = VIEW_MODES.includes(viewParam as ViewMode)
    ? (viewParam as ViewMode)
    : "board";

  // taskCount/closedTaskCount are aggregated server-side onto each backlog
  // (issue #144), so the collection no longer needs every task in the
  // project just to show a count and a completion ratio.
  //
  // The Unclassified count (issue #152) is fetched the same way the Task
  // collection counts its own Unclassified group: every task with no
  // backlog ("unassigned" is the API's backlog_id value for that, distinct
  // from UNCLASSIFIED_BACKLOG which is this app's own route/filter value).
  // It's independent of the priority/progress/sort params above, which only
  // apply to backlogs.
  //
  // getProject above is left unguarded: without a project there's nothing on
  // this screen to fall back to, so its failure is left to bubble up to
  // error.tsx (issue #93), the same as every other project screen. getBacklogs
  // (and the Unclassified count alongside it) is caught here instead, mirroring
  // getTasks in tasks/page.tsx, so a sync/DB hiccup on just this fetch doesn't
  // take the New backlog action and the rest of the screen down with it (issue
  // #155).
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let unclassifiedCount = 0;
  let backlogsError = false;
  try {
    const [fetchedBacklogs, unclassifiedTasks] = await Promise.all([
      getBacklogs(projectId, {
        priority,
        progress,
        sort: sort === "priority" || sort === "progress" ? sort : undefined,
      }),
      getTasks(projectId, { backlogId: "unassigned" }),
    ]);
    backlogs = fetchedBacklogs;
    unclassifiedCount = unclassifiedTasks.length;
  } catch {
    backlogsError = true;
  }

  // The linked GitLab projects a backlog can name as its own issue
  // destination (issue #180). Caught separately from the backlogs above: a
  // project with no GitLab connection has none, and that is the same
  // empty-list outcome as a failed fetch — either way the create/edit forms
  // simply drop the field rather than taking the screen down.
  let links: Awaited<ReturnType<typeof getLinkedGitlabProjects>> = [];
  try {
    links = await getLinkedGitlabProjects(projectId);
  } catch {
    links = [];
  }

  return (
    <BacklogListSection
      projectId={project.id}
      backlogs={backlogs}
      links={links}
      priorityFilter={priority}
      progressFilter={progress}
      sort={sort}
      unclassifiedCount={unclassifiedCount}
      initialView={initialView}
      error={backlogsError}
    />
  );
}
