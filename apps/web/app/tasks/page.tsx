import { redirect } from "next/navigation";
import { getAllTasks, getCurrentUser, getProjects } from "@/lib/api";
import { AppHeader } from "@/components/AppHeader";
import { AllTasksSection } from "@/components/AllTasksSection";
import type { Priority, TaskStatus } from "@/types";

const SORTS = ["dueOn", "priority", "updatedAt"] as const;
type Sort = (typeof SORTS)[number];

const STATUSES = ["all", "open", "closed"] as const;
type StatusFilter = (typeof STATUSES)[number];

const PRIORITIES = ["low", "medium", "high", "urgent"] as const;

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

  let tasks: Awaited<ReturnType<typeof getAllTasks>> = [];
  let projects: Awaited<ReturnType<typeof getProjects>> = [];
  let error = false;
  try {
    [tasks, projects] = await Promise.all([
      getAllTasks({
        status: status === "all" ? undefined : (status as TaskStatus),
        priority,
        sort,
        projectIds: projectIds.length > 0 ? projectIds : undefined,
        dueBefore,
        dueAfter,
        startedBefore,
        assignee: assigneeMe ? "me" : undefined,
      }),
      getProjects(),
    ]);
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
            error={error}
          />
        </div>
      </main>
    </>
  );
}
