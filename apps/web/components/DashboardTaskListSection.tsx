import Link from "next/link";
import { formatDate } from "@/lib/dates";
import { taskPath } from "@/lib/routes";
import type { TaskWithProject } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { PriorityBadge } from "@/components/PriorityBadge";

const MAX_ROWS = 5;

/**
 * DashboardTaskListSection is the shape shared by the dashboard's overdue,
 * due-soon, waiting-to-start and high-priority sections (issue #77): each is
 * a capped, read-only teaser onto the cross-project Task collection,
 * filtered differently server-side and linking out to `/tasks` with the
 * matching filter in the URL (docs/ui-design.md rule 5). No actions live
 * here — closing or editing a task stays on its own single view (rule 4).
 */
export function DashboardTaskListSection({
  title,
  tasks,
  viewAllHref,
  emptyMessage,
  dateField = "dueOn",
}: {
  title: string;
  tasks: TaskWithProject[];
  viewAllHref: string;
  emptyMessage: string;
  /** Which date to show per row: a task's due date, or the date it became
   *  workable. Both come from the same Task shape; only the label differs. */
  dateField?: "dueOn" | "startDate";
}) {
  const visible = tasks.slice(0, MAX_ROWS);
  const dateLabel = dateField === "dueOn" ? "Due" : "Started";

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">
            {title}
            {tasks.length > 0 ? (
              <span className="text-muted-foreground ml-1 font-normal">({tasks.length})</span>
            ) : null}
          </CardTitle>
          {tasks.length > 0 ? (
            <Link
              href={viewAllHref}
              className="text-muted-foreground hover:text-foreground text-xs hover:underline"
            >
              View all
            </Link>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {tasks.length === 0 ? (
          <p className="text-muted-foreground text-sm">{emptyMessage}</p>
        ) : (
          <ul className="space-y-2">
            {visible.map((task) => {
              const date = task[dateField];
              return (
                <li key={task.id}>
                  <Link
                    href={taskPath(task.projectId, task.id)}
                    className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                  >
                    <span className="flex min-w-0 flex-col">
                      <span className="text-foreground truncate">{task.title}</span>
                      <span className="text-muted-foreground text-xs">{task.projectName}</span>
                    </span>
                    <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                      {date ? (
                        <span>
                          {dateLabel} {formatDate(date)}
                        </span>
                      ) : null}
                      <PriorityBadge priority={task.priority} />
                    </span>
                  </Link>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
