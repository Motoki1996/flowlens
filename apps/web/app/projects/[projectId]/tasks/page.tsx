import { notFound } from "next/navigation";
import { getBacklogs, getProject, getTaskDependencies, getTasks } from "@/lib/api";
import { UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { TaskListSection } from "@/components/TaskListSection";
import type { Progress, TaskStatus } from "@/types";

const STATUSES = ["all", "open", "closed"] as const;
type StatusFilter = (typeof STATUSES)[number];

const PROGRESSES = ["not_started", "in_progress", "on_hold", "done"] as const;

// "manual" is the API's own position order, which it expresses by the absence
// of ?sort= rather than a named value; the other four are the ones both Task
// collections accept (see parseTaskListFilter, internal/http/task_handler.go).
const SORTS = ["dueOn", "priority", "progress", "updatedAt"] as const;
type NamedSort = (typeof SORTS)[number];
type Sort = "manual" | NamedSort;

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
    q?: string;
    status?: string;
    progress?: string;
    sort?: string;
  }>;
}) {
  const { projectId } = await params;
  const resolvedSearchParams = await searchParams;
  const backlogFilter = normalizeBacklogFilter(resolvedSearchParams?.backlog);
  const search = resolvedSearchParams?.q;
  const statusParam = resolvedSearchParams?.status;
  const status: StatusFilter = STATUSES.includes(statusParam as StatusFilter)
    ? (statusParam as StatusFilter)
    : "open";
  const progressParam = resolvedSearchParams?.progress;
  const progress = PROGRESSES.includes(progressParam as Progress)
    ? (progressParam as Progress)
    : undefined;
  const sortParam = resolvedSearchParams?.sort;
  const sort: Sort = SORTS.includes(sortParam as NamedSort) ? (sortParam as NamedSort) : "manual";
  const project = await getProject(projectId);
  if (!project) notFound();

  let tasks: Awaited<ReturnType<typeof getTasks>> = [];
  let backlogs: Awaited<ReturnType<typeof getBacklogs>> = [];
  let tasksError = false;
  try {
    [tasks, backlogs] = await Promise.all([
      getTasks(projectId, {
        // The UI's Unclassified group is the API's "unassigned" backlog_id;
        // "all" is no filter at all.
        backlogId:
          backlogFilter === "all"
            ? undefined
            : backlogFilter === UNCLASSIFIED_BACKLOG
              ? "unassigned"
              : backlogFilter,
        status: status === "all" ? undefined : (status as TaskStatus),
        progress,
        sort: sort === "manual" ? undefined : sort,
        q: search,
      }),
      getBacklogs(projectId),
    ]);
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

  return (
    <TaskListSection
      projectId={project.id}
      tasks={tasks}
      backlogs={backlogs}
      dependencies={taskDependencies}
      backlogFilter={backlogFilter}
      search={search}
      statusFilter={status}
      progressFilter={progress}
      sort={sort}
      error={tasksError}
    />
  );
}
