"use client";

import { useMemo } from "react";
import Link from "next/link";
import type { Backlog, Task } from "@/types";
import { backlogPath } from "@/lib/routes";
import {
  backlogProgress,
  computeTimelineBounds,
  hasSchedule,
  spanDays,
  toBacklogGanttRows,
} from "@/lib/timeline";
import { AXIS_HEIGHT, GanttChart, percent, ROW_HEIGHT, STATE_LABEL } from "@/components/GanttChart";
import { PriorityBadge } from "@/components/PriorityBadge";

/** The name column is a fixed width so every row's bar starts at the same x,
 *  and the plot gets a minimum width per day so a long project scrolls
 *  horizontally instead of compressing every bar into a sliver. Both match
 *  TaskTimelineSection so the two timelines read as the same chart. */
const NAME_COLUMN_WIDTH = 200;
const MIN_DAY_WIDTH = 16;
const MIN_PLOT_WIDTH = 480;

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
 * BacklogTimelineSection is the Gantt-chart view mode of the Backlog
 * collection: the same backlogs BacklogListSection shows, laid out as bars
 * along a date axis (docs/ui-design.md rule 5, "a collection is one dataset,
 * presented several ways").
 *
 * Each bar is filled by the share of its tasks that are closed, so a plan and
 * its actual progress are read in one place. The ratio is stated as text in
 * the name column too — the fill is a second reading of it, never the only one.
 */
export function BacklogTimelineSection({
  projectId,
  backlogs,
  tasks,
  tasksError = false,
  now,
}: {
  projectId: string;
  backlogs: Backlog[];
  tasks: Task[];
  /** Progress is unknowable when the task fetch failed, so the chart says so
   *  instead of drawing every backlog as 0% done. */
  tasksError?: boolean;
  /** Injectable so stories and tests pin "today" instead of drifting with the clock. */
  now?: Date;
}) {
  // Defaulting inside a memo rather than in the parameter list keeps "today"
  // stable across renders — a fresh Date() per render would invalidate every
  // memo below it and redraw the chart continuously.
  const today = useMemo(() => now ?? new Date(), [now]);
  const bounds = useMemo(() => computeTimelineBounds(backlogs), [backlogs]);
  const rows = useMemo(
    () => (bounds ? toBacklogGanttRows(backlogs.filter(hasSchedule), tasks, bounds, today) : []),
    [backlogs, tasks, bounds, today],
  );
  const unscheduled = backlogs.filter((b) => !hasSchedule(b));

  const overall = useMemo(() => {
    const scheduled = backlogs.filter(hasSchedule);
    return scheduled.reduce(
      (acc, b) => {
        const p = backlogProgress(tasks, b.id);
        return { closed: acc.closed + p.closed, total: acc.total + p.total };
      },
      { closed: 0, total: 0 },
    );
  }, [backlogs, tasks]);

  if (!bounds || rows.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No scheduled backlogs yet. Set a start date or due date on a backlog to see it on the
        timeline.
      </p>
    );
  }

  const plotWidth = Math.max(MIN_PLOT_WIDTH, spanDays(bounds) * MIN_DAY_WIDTH);

  return (
    <div>
      <div className="text-muted-foreground mb-3 flex flex-wrap items-center justify-between gap-2 text-xs">
        <span>
          {formatDate(bounds.start)} – {formatDate(bounds.end)}
        </span>
        {tasksError ? (
          <span className="text-destructive">Failed to load tasks — progress is unavailable.</span>
        ) : (
          <span>
            {overall.closed}/{overall.total} tasks closed
            {overall.total > 0 ? ` (${percent(overall.closed / overall.total)})` : ""}
          </span>
        )}
      </div>

      <div className="flex">
        <div className="shrink-0" style={{ width: NAME_COLUMN_WIDTH }}>
          {/* Spacer keeping the first name aligned with the first bar, not with the date axis. */}
          <div style={{ height: AXIS_HEIGHT }} />
          <ul>
            {rows.map((row) => (
              <li
                key={row.id}
                className="flex flex-col justify-center pr-3"
                style={{ height: ROW_HEIGHT }}
              >
                <div className="flex min-w-0 items-center gap-1.5">
                  <Link
                    href={backlogPath(projectId, row.id)}
                    className="text-foreground truncate text-sm hover:underline"
                    title={row.title}
                  >
                    {row.title}
                  </Link>
                  <PriorityBadge priority={row.priority} />
                </div>
                {!tasksError && row.progress ? (
                  <span className="text-muted-foreground truncate text-xs tabular-nums">
                    {row.progress.total === 0
                      ? "No tasks"
                      : `${row.progress.closed}/${row.progress.total} closed (${percent(row.progress.ratio)})`}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        </div>

        <div className="min-w-0 flex-1 overflow-x-auto">
          <div style={{ minWidth: plotWidth }}>
            <GanttChart
              rows={rows}
              bounds={bounds}
              now={today}
              href={(row) => backlogPath(projectId, row.id)}
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
        <li className="flex items-center gap-1.5">
          <span
            aria-hidden
            className="bg-muted-foreground/25 size-2 shrink-0 rounded-[2px]"
          />
          Remaining
        </li>
      </ul>

      {unscheduled.length > 0 ? (
        <p className="text-muted-foreground mt-4 text-xs">
          {unscheduled.length} backlog{unscheduled.length > 1 ? "s have" : " has"} no start or due
          date and {unscheduled.length > 1 ? "aren't" : "isn't"} shown above:{" "}
          {unscheduled.map((b) => b.name).join(", ")}
        </p>
      ) : null}
    </div>
  );
}
