import { notFound } from "next/navigation";
import {
  getBacklogs,
  getEpics,
  getGitlabConnection,
  getMyGitlabIdentities,
  getProject,
  getTaskDependencies,
  getTasks,
} from "@/lib/api";
import { UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { toDateParam } from "@/lib/dates";
import { NO_EPIC, TaskListSection, type AssigneeAvailability } from "@/components/TaskListSection";
import type { ViewMode } from "@/components/ViewModeToggle";
import type { TaskStatus } from "@/types";

const STATUSES = ["all", "open", "closed"] as const;
type StatusFilter = (typeof STATUSES)[number];

// "manual" is the API's own default (creation) order, which it expresses by the absence
// of ?sort= rather than a named value; the other five are the ones both Task
// collections accept (see parseTaskListFilter, internal/http/task_handler.go).
const SORTS = ["dueOn", "priority", "progress", "size", "updatedAt"] as const;
type NamedSort = (typeof SORTS)[number];
type Sort = "manual" | NamedSort;

// The view modes ViewModeToggle offers, shared by Task and Backlog (issue
// #153) — Board is the default and falls out of the query string entirely.
const VIEW_MODES = ["board", "list", "timeline"] as const;

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** normalizeBacklogFilter keeps `?backlog=` to the values the API's
 *  `backlog_id` accepts — a backlog UUID or the Unclassified group — so a
 *  hand-edited one falls back to "all" instead of turning the whole screen
 *  into a load error (the API rejects a malformed backlog_id with a 400). */
function normalizeBacklogFilter(value: string | undefined): string {
  if (!value) return "all";
  if (value === UNCLASSIFIED_BACKLOG || UUID_RE.test(value)) return value;
  return "all";
}

/** normalizeEpicFilter is normalizeBacklogFilter for `?epic=`: an epic UUID,
 *  NO_EPIC for the tasks sitting directly in a backlog, or "all". */
function normalizeEpicFilter(value: string | undefined): string {
  if (!value) return "all";
  if (value === NO_EPIC || UUID_RE.test(value)) return value;
  return "all";
}

/**
 * The Task collection view of one project — Board (the default), List and
 * Timeline are view modes of this one screen, per docs/ui-design.md rule 5,
 * and `?backlog=` is the
 * backlog filter of that same collection. The backlog screens link here rather
 * than keeping a task list of their own.
 *
 * Every filter lives in the URL and is applied by the API, not by the client
 * (issue #143): the query below is the request, so the three view modes are
 * three presentations of one server-filtered set. An unrecognised value falls
 * back to that filter's default rather than erroring, since these come
 * straight from a hand-editable query string.
 *
 * Auth is guarded by the parent layout.tsx; this page doesn't render the
 * AppHeader so it has no need of its own user object.
 */
export default async function TasksPage({
  params,
  searchParams,
}: {
  params: Promise<{ projectId: string }>;
  searchParams?: Promise<{
    backlog?: string;
    epic?: string;
    q?: string;
    status?: string;
    assignee?: string;
    sort?: string;
    view?: string;
  }>;
}) {
  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;
  const backlogFilter = normalizeBacklogFilter(resolvedSearchParams?.backlog);
  const epicFilter = normalizeEpicFilter(resolvedSearchParams?.epic);
  const search = resolvedSearchParams?.q;
  const assigneeMe = resolvedSearchParams?.assignee === "me";
  const statusParam = resolvedSearchParams?.status;
  const status: StatusFilter = STATUSES.includes(statusParam as StatusFilter)
    ? (statusParam as StatusFilter)
    : "open";
  const sortParam = resolvedSearchParams?.sort;
  const sort: Sort = SORTS.includes(sortParam as NamedSort) ? (sortParam as NamedSort) : "manual";
  const viewParam = resolvedSearchParams?.view;
  const initialView: ViewMode = VIEW_MODES.includes(viewParam as ViewMode)
    ? (viewParam as ViewMode)
    : "board";
  // The "today" cutoff the `?due=` filter and each row's/card's Overdue
  // badge classify against (issue #148), computed once here rather than in
  // the client component below — see TaskListSection's `today` prop doc
  // comment for why.
  const today = toDateParam(new Date());
  const project = await getProject(projectId);
  if (!project) notFound();

  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let epics: Awaited<ReturnType<typeof getEpics>> = [];
  let totalCount: number | undefined;
  let tasksError = false;
  try {
    // The unfiltered fetch alongside the filtered one is the "母数" (issue
    // #150): the project's task count with no filter applied at all, so the
    // screen can show "N / total tasks" instead of just the filtered count.
    const [fetchedTasks, fetchedBacklogs, fetchedEpics, allTasks] = await Promise.all([
      getTasks(projectId, {
        // The UI's Unclassified group is the API's "unassigned" backlog_id;
        // "all" is no filter at all.
        backlogId:
          backlogFilter === "all"
            ? undefined
            : backlogFilter === UNCLASSIFIED_BACKLOG
              ? "unassigned"
              : backlogFilter,
        // The UI says "none"; the API spells the same thing "unassigned" on
        // epic_id, matching backlog_id's own vocabulary.
        epicId:
          epicFilter === "all" ? undefined : epicFilter === NO_EPIC ? "unassigned" : epicFilter,
        status: status === "all" ? undefined : (status as TaskStatus),
        sort: sort === "manual" ? undefined : sort,
        assignee: assigneeMe ? "me" : undefined,
        q: search,
      }),
      // "all": these two are lookup tables for the task rows' backlog/epic
      // labels and the filter dropdowns, not browsable lists. A task filed in
      // a backlog that has since been closed must still name it.
      getBacklogs(projectId, { status: "all" }),
      getEpics(projectId, { status: "all" }),
      getTasks(projectId, {}),
    ]);
    tasks = fetchedTasks;
    backlogs = fetchedBacklogs;
    epics = fetchedEpics;
    totalCount = allTasks.length;
  } catch {
    tasksError = true;
  }

  let taskDependencies: Awaited<ReturnType<typeof getTaskDependencies>> = [];
  try {
    taskDependencies = await getTaskDependencies(projectId);
  } catch {
    // Left empty; the timeline view still renders tasks, just without
    // dependency lines.
  }

  // "My tasks" matches nothing server-side (rather than erroring) when the
  // project has no GitLab connection or the caller has no identity
  // registered for it (issue #146, building on issue #102's ?assignee=me) —
  // that would read as a silent, unexplained empty list, so the checkbox
  // below disables itself and says which of the two is missing instead.
  // Left at its safe "no-connection" default on a lookup failure, same as
  // taskDependencies above.
  let assigneeAvailability: AssigneeAvailability = "no-connection";
  try {
    const connection = await getGitlabConnection(projectId);
    if (connection) {
      const identities = await getMyGitlabIdentities();
      assigneeAvailability = identities.some((identity) => identity.gitlabBaseUrl === connection.baseUrl)
        ? "available"
        : "no-identity";
    }
  } catch {
    // Left at "no-connection".
  }

  return (
    <TaskListSection
      projectId={project.id}
      tasks={tasks}
      backlogs={backlogs}
      epics={epics}
      dependencies={taskDependencies}
      backlogFilter={backlogFilter}
      epicFilter={epicFilter}
      search={search}
      statusFilter={status}
      assigneeMe={assigneeMe}
      assigneeAvailability={assigneeAvailability}
      sort={sort}
      today={today}
      totalCount={totalCount}
      error={tasksError}
      initialView={initialView}
    />
  );
}
