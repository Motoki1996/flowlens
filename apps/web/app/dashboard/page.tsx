import { redirect } from "next/navigation";
import { getAllTasks, getCurrentUser, getFailedSyncProjects, getProjects } from "@/lib/api";
import { toDateParam } from "@/lib/dates";
import { classifyDueTasks, endOfWeek } from "@/lib/dashboard";
import { allTasksPath } from "@/lib/routes";
import { AppHeader } from "@/components/AppHeader";
import { DashboardView } from "@/components/DashboardView";
import type { Project, TaskWithProject } from "@/types";

const DUE_POOL_LIMIT = 50;
const WAITING_LIMIT = 10;
const PRIORITY_POOL_LIMIT = 20;
const HIGH_PRIORITIES = new Set(["urgent", "high"]);

/**
 * DashboardPage is the landing screen after login (issue #77): "what needs
 * attention across every project I own", built entirely from the
 * cross-project Task collection (GET /api/v1/tasks, issue #76) and the
 * Project collection — no object of its own, see DashboardView's doc
 * comment. It fetches everything server-side and hands plain props to the
 * presentational DashboardView, same split as AllTasksPage/AllTasksSection.
 */
export default async function DashboardPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");

  let projects: Project[] = [];
  let projectsError = false;
  try {
    projects = await getProjects();
  } catch {
    projectsError = true;
  }

  if (projectsError || projects.length === 0) {
    return (
      <>
        <AppHeader user={user} />
        <main className="mx-auto max-w-6xl px-6 py-8">
          <h1 className="text-foreground text-2xl font-semibold">Dashboard</h1>
          <p className="text-muted-foreground mt-1 text-sm">Signed in as {user.username}.</p>
          <DashboardView
            hasProjects={projects.length > 0}
            error={projectsError}
            overdueTasks={[]}
            dueSoonTasks={[]}
            waitingTasks={[]}
            priorityTasks={[]}
            showDueDateHint={false}
            failedSyncProjects={[]}
            recentProjects={[]}
            overdueHref={allTasksPath()}
            dueSoonHref={allTasksPath()}
            waitingHref={allTasksPath()}
            priorityHref={allTasksPath()}
          />
        </main>
      </>
    );
  }

  const now = new Date();
  const today = toDateParam(now);
  const yesterday = toDateParam(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1));
  const weekEnd = toDateParam(endOfWeek(now));

  let duePool: TaskWithProject[] = [];
  let waitingTasks: TaskWithProject[] = [];
  let priorityPool: TaskWithProject[] = [];
  let failedSyncProjects: Project[] = [];
  let error = false;
  try {
    [duePool, waitingTasks, priorityPool, failedSyncProjects] = await Promise.all([
      // Sorted by due date ascending (nulls last): the same fetch covers
      // both "overdue" and "due this week", split client-side below.
      getAllTasks({ status: "open", sort: "dueOn", limit: DUE_POOL_LIMIT }),
      getAllTasks({ status: "open", startedBefore: today, sort: "dueOn", limit: WAITING_LIMIT }),
      getAllTasks({ status: "open", sort: "priority", limit: PRIORITY_POOL_LIMIT }),
      getFailedSyncProjects(),
    ]);
  } catch {
    error = true;
  }

  const { overdue, dueSoon } = classifyDueTasks(duePool, now);
  const priorityTasks = priorityPool.filter((t) => HIGH_PRIORITIES.has(t.priority));
  const recentProjects = [...projects].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
  const showDueDateHint = duePool.length > 0 && !duePool.some((t) => t.dueOn);

  const tasksQuery = `${allTasksPath()}?status=open&sort=dueOn`;

  return (
    <>
      <AppHeader user={user} />
      <main className="mx-auto max-w-6xl px-6 py-8">
        <h1 className="text-foreground text-2xl font-semibold">Dashboard</h1>
        <p className="text-muted-foreground mt-1 text-sm">Signed in as {user.username}.</p>
        <DashboardView
          hasProjects
          error={error}
          overdueTasks={overdue}
          dueSoonTasks={dueSoon}
          waitingTasks={waitingTasks}
          priorityTasks={priorityTasks}
          showDueDateHint={showDueDateHint}
          failedSyncProjects={failedSyncProjects}
          recentProjects={recentProjects}
          overdueHref={`${tasksQuery}&dueBefore=${yesterday}`}
          dueSoonHref={`${tasksQuery}&dueAfter=${today}&dueBefore=${weekEnd}`}
          waitingHref={`${allTasksPath()}?status=open&sort=dueOn&startedBefore=${today}`}
          priorityHref={`${allTasksPath()}?status=open&sort=priority`}
        />
      </main>
    </>
  );
}
