import Link from "next/link";
import type { Project, TaskWithProject } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { DashboardTaskListSection } from "@/components/DashboardTaskListSection";
import { DashboardSyncFailuresSection } from "@/components/DashboardSyncFailuresSection";
import { DashboardRecentProjectsSection } from "@/components/DashboardRecentProjectsSection";

/**
 * DashboardView is the presentational half of /dashboard (issue #77): a set
 * of read-only teasers onto the cross-project Task collection and the
 * Project collection, each linking out to its own collection view with the
 * matching filter pre-set in the URL. It is not a view of an object of its
 * own — docs/ui-design.md's screen map deliberately doesn't list it as one
 * — and it carries no edit actions (rule 4); those stay on the Task and
 * Project single views the sections link to.
 */
export function DashboardView({
  hasProjects,
  overdueTasks,
  dueSoonTasks,
  waitingTasks,
  priorityTasks,
  showDueDateHint,
  failedSyncProjects,
  recentProjects,
  overdueHref,
  dueSoonHref,
  waitingHref,
  priorityHref,
  error = false,
}: {
  hasProjects: boolean;
  overdueTasks: TaskWithProject[];
  dueSoonTasks: TaskWithProject[];
  waitingTasks: TaskWithProject[];
  priorityTasks: TaskWithProject[];
  /** True when the user has open tasks but none of them has a due date —
   *  the overdue/due-soon empty states then explain what setting one would
   *  surface, instead of implying "nothing overdue" (issue #77's empty
   *  state for a project with tasks but no due dates). */
  showDueDateHint: boolean;
  failedSyncProjects: Project[];
  recentProjects: Project[];
  overdueHref: string;
  dueSoonHref: string;
  waitingHref: string;
  priorityHref: string;
  error?: boolean;
}) {
  if (!hasProjects) {
    return (
      <Card className="mt-8 border-dashed">
        <CardHeader className="items-center text-center">
          <CardTitle className="text-base font-medium">No projects yet</CardTitle>
          <CardDescription className="mx-auto max-w-md">
            Create a project to start tracking its tasks here.
          </CardDescription>
          <Button asChild className="mt-2">
            <Link href="/projects">Create a project</Link>
          </Button>
        </CardHeader>
      </Card>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive" className="mt-8">
        <AlertDescription>Failed to load the dashboard. Try refreshing the page.</AlertDescription>
      </Alert>
    );
  }

  const dueDateHint = "Set a due date on a task to see it here.";

  return (
    <div className="mt-8 grid grid-cols-1 gap-4 lg:grid-cols-2">
      <DashboardTaskListSection
        title="Overdue"
        tasks={overdueTasks}
        viewAllHref={overdueHref}
        emptyMessage={showDueDateHint ? dueDateHint : "No overdue tasks."}
      />
      <DashboardTaskListSection
        title="Due today / this week"
        tasks={dueSoonTasks}
        viewAllHref={dueSoonHref}
        emptyMessage={showDueDateHint ? dueDateHint : "Nothing due this week."}
      />
      <DashboardTaskListSection
        title="Waiting to start"
        tasks={waitingTasks}
        viewAllHref={waitingHref}
        emptyMessage="No tasks are waiting to start."
        dateField="startDate"
      />
      <DashboardTaskListSection
        title="High priority"
        tasks={priorityTasks}
        viewAllHref={priorityHref}
        emptyMessage="No high-priority open tasks."
      />
      <DashboardSyncFailuresSection projects={failedSyncProjects} />
      <DashboardRecentProjectsSection projects={recentProjects} />
    </div>
  );
}
