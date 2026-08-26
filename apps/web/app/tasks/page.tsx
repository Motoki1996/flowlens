import { redirect } from "next/navigation";
import { getAllTasks, getCurrentUser, getProjects } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { AllTasksSection } from "@/components/AllTasksSection";
import type { Priority, Progress, TaskStatus, TaskWithProject } from "@/types";

// One page of the cross-project collection. It replaces the old bare
// ?limit=50, which capped the list with no way to reach anything past it.
const TASKS_PER_PAGE = 50;

const SORTS = ["dueOn", "priority", "progress", "updatedAt"] as const;
type Sort = (typeof SORTS)[number];

const STATUSES = ["all", "open", "closed"] as const;
type StatusFilter = (typeof STATUSES)[number];

const PRIORITIES = ["low", "medium", "high", "urgent"] as const;

const PROGRESSES = ["not_started", "in_progress", "on_hold", "done"] as const;

function firstParam(value: string | string[] | undefined) {
  return Array.isArray(value) ? value[0] : value;
}

/**
 * The cross-project Task collection (issue #76): every task across every
 * project the user owns, "what should I be doing right now" without opening
 * each project in turn — see docs/ui-design.md's screen map. Status, priority
 * and sort live in the URL, mirroring /projects/[projectId]/tasks's own
 * `?backlog=` hand-off (docs/ui-design.md rule 5); the default (no query at
 * all) is open tasks sorted by due date, per this issue's completion
 * condition.
 */
export default async function AllTasksPage({
  searchParams,
}: {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}) {
  // middleware.ts only checks that the session cookie exists; this page
  // needs the actual user object below (for AppHeader), and this is also the
  // fallback that redirects when the cookie is present but expired/invalid.
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  const params = (await searchParams) ?? {};
  const statusParam = firstParam(params.status);
  const status: StatusFilter = STATUSES.includes(statusParam as StatusFilter)
    ? (statusParam as StatusFilter)
    : "open";
  const sortParam = firstParam(params.sort);
  const sort: Sort = SORTS.includes(sortParam as Sort) ? (sortParam as Sort) : "dueOn";
  const priorityParam = firstParam(params.priority);
  const priority = PRIORITIES.includes(priorityParam as Priority)
    ? (priorityParam as Priority)
    : undefined;
  // Progress has no control of its own on this screen — it is here so the
  // filter set matches what GET /api/v1/tasks accepts, deep-linkable the same
  // way dueBefore/dueAfter below already are.
  const progressParam = firstParam(params.progress);
  const progress = PROGRESSES.includes(progressParam as Progress)
    ? (progressParam as Progress)
    : undefined;
  const projectIds = (Array.isArray(params.projectId) ? params.projectId : [params.projectId]).filter(
    (v): v is string => Boolean(v),
  );
  // dueBefore/dueAfter/startedBefore have no filter control of their own on
  // this screen — they exist so the dashboard's overdue/due-soon/
  // waiting-to-start sections (issue #77) can deep-link here pre-filtered,
  // the same hand-off `?backlog=` already does for the project-scoped Task
  // collection.
  const dueBefore = firstParam(params.dueBefore);
  const dueAfter = firstParam(params.dueAfter);
  const startedBefore = firstParam(params.startedBefore);
  const assigneeMe = firstParam(params.assignee) === "me";
  const search = firstParam(params.q);

  const page = Number(firstParam(params.page)) || 1;

  let tasks: TaskWithProject[] = [];
  let totalCount = 0;
  let nextPage = 0;
  let projects: Awaited<ReturnType<typeof getProjects>> = [];
  let error = false;
  try {
    const [taskPage, fetchedProjects] = await Promise.all([
      getAllTasks({
        status: status === "all" ? undefined : (status as TaskStatus),
        priority,
        progress,
        sort,
        projectIds: projectIds.length > 0 ? projectIds : undefined,
        dueBefore,
        dueAfter,
        startedBefore,
        assignee: assigneeMe ? "me" : undefined,
        q: search,
        page,
        perPage: TASKS_PER_PAGE,
      }),
      getProjects(),
    ]);
    tasks = taskPage.tasks;
    totalCount = taskPage.totalCount;
    nextPage = taskPage.nextPage;
    projects = fetchedProjects;
  } catch {
    error = true;
  }

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <h1 className="text-foreground text-2xl font-semibold">Tasks</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Every task across every project you own.
        </p>
        <div className="mt-6">
          <AllTasksSection
            tasks={tasks}
            projects={projects}
            status={status}
            priority={priority}
            sort={sort}
            assigneeMe={assigneeMe}
            search={search}
            totalCount={totalCount}
            page={page}
            perPage={TASKS_PER_PAGE}
            nextPage={nextPage}
            error={error}
          />
        </div>
      </main>
    </>
  );
}
