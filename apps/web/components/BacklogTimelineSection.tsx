"use client";

import { useMemo } from "react";
import Link from "next/link";
import type { Backlog } from "@/types";
import { backlogPath } from "@/lib/routes";
import {
  backlogTaskCompletion,
  computeTimelineBounds,
  hasSchedule,
  toBacklogGanttRows,
} from "@/lib/timeline";
import { useTimelineViewport } from "@/lib/useTimelineViewport";
import {
  AXIS_HEIGHT,
  GanttChart,
  NAME_COLUMN_CLASS,
  percent,
  ROW_HEIGHT,
  STATE_LABEL,
} from "@/components/GanttChart";
import { TimelineControls } from "@/components/TimelineControls";
import { PriorityFlag } from "@/components/PriorityBadge";

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
  now,
}: {
  projectId: string;
  backlogs: Backlog[];
  /** Injectable so stories and tests pin "today" instead of drifting with the clock. */
  now?: Date;
}) {
  // Defaulting inside a memo rather than in the parameter list keeps "today"
  // stable across renders — a fresh Date() per render would invalidate every
  // memo below it and redraw the chart continuously.
  const today = useMemo(() => now ?? new Date(), [now]);
  const bounds = useMemo(() => computeTimelineBounds(backlogs), [backlogs]);
  const rows = useMemo(
    () => (bounds ? toBacklogGanttRows(backlogs.filter(hasSchedule), bounds, today) : []),
    [backlogs, bounds, today],
  );
  const unscheduled = backlogs.filter((b) => !hasSchedule(b));
  const viewport = useTimelineViewport(bounds, today);

  const overall = useMemo(() => {
    const scheduled = backlogs.filter(hasSchedule);
    return scheduled.reduce(
      (acc, b) => {
        const p = backlogTaskCompletion(b);
        return { closed: acc.closed + p.closed, total: acc.total + p.total };
      },
      { closed: 0, total: 0 },
    );
  }, [backlogs]);

  if (!bounds || rows.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No scheduled backlogs yet. Set a start date or due date on a backlog to see it on the
        timeline.
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
            {overall.closed}/{overall.total} tasks closed
            {overall.total > 0 ? ` (${percent(overall.closed / overall.total)})` : ""}
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
        <div className={NAME_COLUMN_CLASS}>
          {/* Spacer keeping the first name aligned with the first bar, not with the date axis. */}
          <div style={{ height: AXIS_HEIGHT }} />
          <ul>
            {rows.map((row) => (
              <li
                key={row.id}
                className="flex flex-col justify-center pr-3"
                style={{ height: ROW_HEIGHT }}
              >
                {/* The title gets the line to itself — see TaskTimelineSection
                    for why the priority and progress pills left it unreadable.
                    Both are on the bar's tooltip instead. */}
                <Link
                  href={backlogPath(projectId, row.id)}
                  className="text-foreground truncate text-sm hover:underline"
                  title={row.title}
                >
                  {row.title}
                </Link>
                <div className="flex min-w-0 items-center gap-1.5 text-xs">
                  <PriorityFlag priority={row.priority} />
                  {row.completion ? (
                    <span className="text-muted-foreground truncate tabular-nums">
                      {row.completion.total === 0
                        ? "No tasks"
                        : `${row.completion.closed}/${row.completion.total} closed (${percent(row.completion.ratio)})`}
                    </span>
                  ) : null}
                </div>
              </li>
            ))}
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
