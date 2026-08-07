"use client";

import { useMemo } from "react";
import Link from "next/link";
import type { Task, TaskDependency } from "@/types";
import { taskPath } from "@/lib/routes";
import { computeTimelineBounds, hasSchedule, toTaskGanttRows } from "@/lib/timeline";
import { useTimelineViewport } from "@/lib/useTimelineViewport";
import { AXIS_HEIGHT, GanttChart, ROW_HEIGHT, STATE_LABEL } from "@/components/GanttChart";
import { TimelineControls } from "@/components/TimelineControls";
import { PriorityBadge } from "@/components/PriorityBadge";
import { ProgressBadge } from "@/components/ProgressBadge";

/** The name column is a fixed width so every row's bar starts at the same x.
 *  The plot's own width comes from the zoom level (see useTimelineViewport):
 *  past the container it scrolls horizontally rather than compressing the bars. */
const NAME_COLUMN_WIDTH = 200;

function formatDate(date: Date) {
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

const LEGEND_STATES = ["open", "overdue", "closed"] as const;

const LEGEND_SWATCH: Record<(typeof LEGEND_STATES)[number], string> = {
  open: "var(--chart-1)",
  overdue: "var(--destructive)",
  closed: "var(--muted-foreground)",
};

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
  projectId,
  tasks,
  allTasks,
  dependencies,
  now,
}: {
  projectId: string;
  tasks: Task[];
  /** Every task in the collection, filters included — `tasks` may be a filtered
   *  subset, and a predecessor filtered out of view still has to be nameable in
   *  the "After: …" line. Defaults to `tasks` when nothing is filtered out. */
  allTasks?: Task[];
  dependencies: TaskDependency[];
  /** Injectable so stories and tests pin "today" instead of drifting with the clock. */
  now?: Date;
}) {
  // Defaulting inside a memo rather than in the parameter list keeps "today"
  // stable across renders — a fresh Date() per render would invalidate every
  // memo below it and redraw the chart continuously.
  const today = useMemo(() => now ?? new Date(), [now]);
  const bounds = useMemo(() => computeTimelineBounds(tasks), [tasks]);
  const rows = useMemo(
    () => (bounds ? toTaskGanttRows(tasks.filter(hasSchedule), bounds, today) : []),
    [tasks, bounds, today],
  );
  const unscheduled = tasks.filter((t) => !hasSchedule(t));
  const viewport = useTimelineViewport(bounds, today);

  const predecessorsByTask = useMemo(() => {
    const titleById = new Map((allTasks ?? tasks).map((t) => [t.id, t.title]));
    const byTask = new Map<string, string[]>();
    for (const d of dependencies) {
      const predecessorTitle = titleById.get(d.predecessorTaskId);
      if (!predecessorTitle) continue;
      const list = byTask.get(d.successorTaskId) ?? [];
      list.push(predecessorTitle);
      byTask.set(d.successorTaskId, list);
    }
    return byTask;
  }, [tasks, allTasks, dependencies]);

  const closedCount = tasks.filter((t) => t.status === "closed").length;
  const progressPercent = tasks.length > 0 ? Math.round((closedCount / tasks.length) * 100) : 0;

  if (!bounds || rows.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No scheduled tasks yet. Set a start date or due date on a task to see it on the timeline.
      </p>
    );
  }

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
          <span>
            {formatDate(bounds.start)} – {formatDate(bounds.end)}
          </span>
          <span>
            {closedCount}/{tasks.length} closed ({progressPercent}%)
          </span>
        </div>
        <TimelineControls
          zoom={viewport.zoom}
          onZoomChange={viewport.setZoom}
          onToday={viewport.scrollToToday}
          hasToday={viewport.hasToday}
        />
      </div>

      <div className="flex">
        <div className="shrink-0" style={{ width: NAME_COLUMN_WIDTH }}>
          {/* Spacer keeping the first name aligned with the first bar, not with the date axis. */}
          <div style={{ height: AXIS_HEIGHT }} />
          <ul>
            {rows.map((row) => {
              const predecessors = predecessorsByTask.get(row.id);
              return (
                <li
                  key={row.id}
                  className="flex flex-col justify-center pr-3"
                  style={{ height: ROW_HEIGHT }}
                >
                  <div className="flex min-w-0 items-center gap-1.5">
                    <Link
                      href={taskPath(projectId, row.id)}
                      className="text-foreground truncate text-sm hover:underline"
                      title={row.title}
                    >
                      {row.title}
                    </Link>
                    <PriorityBadge priority={row.priority} />
                    <ProgressBadge progress={row.progress} />
                  </div>
                  {predecessors ? (
                    <span className="text-muted-foreground truncate text-xs">
                      After: {predecessors.join(", ")}
                    </span>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </div>

        <div
          ref={viewport.scrollRef}
          onScroll={viewport.onScroll}
          className="min-w-0 flex-1 overflow-x-auto"
        >
          <div style={{ minWidth: viewport.plotWidth }}>
            <GanttChart
              rows={rows}
              bounds={bounds}
              now={today}
              zoom={viewport.zoom}
              href={(row) => taskPath(projectId, row.id)}
            />
          </div>
        </div>
      </div>

      <ul
        aria-label="Bar colours"
        className="text-muted-foreground mt-3 flex flex-wrap items-center gap-4 text-xs"
      >
        {LEGEND_STATES.map((state) => (
          <li key={state} className="flex items-center gap-1.5">
            <span
              aria-hidden
              className="size-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: LEGEND_SWATCH[state] }}
            />
            {STATE_LABEL[state]}
          </li>
        ))}
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
