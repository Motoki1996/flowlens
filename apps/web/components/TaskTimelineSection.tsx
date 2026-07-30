import Link from "next/link";
import type { Task, TaskDependency } from "@/types";
import { barStyle, computeTimelineBounds, effectiveRange, hasSchedule } from "@/lib/timeline";

function formatDate(date: Date) {
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/**
 * TaskTimelineSection is the Gantt-chart view mode of the Task collection
 * (issue #33): the same tasks TaskListSection shows, laid out as bars along
 * a date axis instead of grouped rows, per docs/ui-design.md rule 5 ("a
 * collection is one dataset, presented several ways"). Progress is the
 * closed/total task ratio (docs/plans decision: no separate manual
 * progress field), and dependencies are shown as "After: <predecessors>"
 * under each task that has one, since GitLab Issues have no native
 * predecessor/successor concept to render arrows from.
 */
export function TaskTimelineSection({
  tasks,
  dependencies,
}: {
  tasks: Task[];
  dependencies: TaskDependency[];
}) {
  const scheduled = tasks
    .filter(hasSchedule)
    .sort((a, b) => effectiveRange(a)!.start.getTime() - effectiveRange(b)!.start.getTime());
  const unscheduled = tasks.filter((t) => !hasSchedule(t));
  const bounds = computeTimelineBounds(tasks);

  const titleById = new Map(tasks.map((t) => [t.id, t.title]));
  const predecessorsByTask = new Map<string, string[]>();
  for (const d of dependencies) {
    const predecessorTitle = titleById.get(d.predecessorTaskId);
    if (!predecessorTitle) continue;
    const list = predecessorsByTask.get(d.successorTaskId) ?? [];
    list.push(predecessorTitle);
    predecessorsByTask.set(d.successorTaskId, list);
  }

  const closedCount = tasks.filter((t) => t.status === "closed").length;
  const progressPercent = tasks.length > 0 ? Math.round((closedCount / tasks.length) * 100) : 0;

  if (!bounds || scheduled.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No scheduled tasks yet. Set a start date or due date on a task to see it on the timeline.
      </p>
    );
  }

  return (
    <div>
      <div className="text-muted-foreground mb-3 flex flex-wrap items-center justify-between gap-2 text-xs">
        <span>
          {formatDate(bounds.start)} – {formatDate(bounds.end)}
        </span>
        <span>
          {closedCount}/{tasks.length} closed ({progressPercent}%)
        </span>
      </div>

      <ul className="space-y-3">
        {scheduled.map((task) => {
          const style = barStyle(task, bounds);
          const predecessors = predecessorsByTask.get(task.id);
          return (
            <li key={task.id}>
              <div className="mb-1 flex items-center justify-between gap-2">
                <Link
                  href={`/tasks/${task.id}`}
                  className="text-foreground text-sm hover:underline"
                >
                  {task.title}
                </Link>
                {predecessors ? (
                  <span className="text-muted-foreground text-xs">After: {predecessors.join(", ")}</span>
                ) : null}
              </div>
              <div className="bg-muted relative h-2.5 w-full overflow-hidden rounded-full">
                {style ? (
                  <div
                    className={`absolute top-0 h-full rounded-full ${
                      task.status === "closed" ? "bg-muted-foreground/60" : "bg-primary"
                    }`}
                    style={{ left: `${style.leftPercent}%`, width: `${style.widthPercent}%` }}
                    title={`${task.title}: ${
                      task.startDate ? formatDate(new Date(task.startDate)) : "?"
                    } – ${task.dueOn ? formatDate(new Date(task.dueOn)) : "?"}`}
                  />
                ) : null}
              </div>
            </li>
          );
        })}
      </ul>

      {unscheduled.length > 0 ? (
        <p className="text-muted-foreground mt-4 text-xs">
          {unscheduled.length} task{unscheduled.length > 1 ? "s have" : " has"} no start or due date and
          {unscheduled.length > 1 ? " aren't" : " isn't"} shown above: {unscheduled.map((t) => t.title).join(", ")}
        </p>
      ) : null}
    </div>
  );
}
