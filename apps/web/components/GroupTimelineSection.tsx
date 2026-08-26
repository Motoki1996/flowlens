"use client";

import { useMemo } from "react";
import {
  computeTimelineBounds,
  formatUnscheduledNames,
  groupTaskCompletion,
  hasSchedule,
  toGroupGanttRows,
} from "@/lib/timeline";
import { GROUP_CONFIG, type GroupKind, type Grouping } from "@/lib/groups";
import { useTimelineViewport } from "@/lib/useTimelineViewport";
import { percent, STATE_LABEL } from "@/components/GanttChart";
import { TimelineFrame } from "@/components/TimelineFrame";
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
 * GroupTimelineSection is the Gantt-chart view mode shared by the Backlog and
 * Epic collections (lib/groups.ts): the same objects the List mode shows,
 * laid out as bars along a date axis (docs/ui-design.md rule 5, "a collection
 * is one dataset, presented several ways").
 *
 * Each bar is filled by the share of its tasks that are closed, so a plan and
 * its actual progress are read in one place. The ratio is stated as text in
 * the name column too — the fill is a second reading of it, never the only one.
 */
export function GroupTimelineSection({
  projectId,
  kind,
  items: backlogs,
  now,
}: {
  projectId: string;
  kind: GroupKind;
  items: Grouping[];
  /** Injectable so stories and tests pin "today" instead of drifting with the clock. */
  now?: Date;
}) {
  const config = GROUP_CONFIG[kind];
  // Defaulting inside a memo rather than in the parameter list keeps "today"
  // stable across renders — a fresh Date() per render would invalidate every
  // memo below it and redraw the chart continuously.
  const today = useMemo(() => now ?? new Date(), [now]);
  const bounds = useMemo(() => computeTimelineBounds(backlogs), [backlogs]);
  const rows = useMemo(
    () => (bounds ? toGroupGanttRows(backlogs.filter(hasSchedule), bounds, today) : []),
    [backlogs, bounds, today],
  );
  const unscheduled = backlogs.filter((b) => !hasSchedule(b));
  const viewport = useTimelineViewport(bounds, today);

  const overall = useMemo(() => {
    const scheduled = backlogs.filter(hasSchedule);
    return scheduled.reduce(
      (acc, b) => {
        const p = groupTaskCompletion(b);
        return { closed: acc.closed + p.closed, total: acc.total + p.total };
      },
      { closed: 0, total: 0 },
    );
  }, [backlogs]);

  if (!bounds || rows.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No scheduled {config.plural} yet. Set a start date or due date on {config.noun === "epic" ? "an" : "a"}{" "}
        {config.noun} to see it on the timeline.
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

      <TimelineFrame
        rows={rows}
        bounds={bounds}
        now={today}
        viewport={viewport}
        href={(row) => config.detailPath(projectId, row.id)}
        meta={(row) => (
          <>
            <PriorityFlag priority={row.priority} />
            {row.completion ? (
              <span className="text-muted-foreground truncate tabular-nums">
                {row.completion.total === 0
                  ? "No tasks"
                  : `${row.completion.closed}/${row.completion.total} closed (${percent(row.completion.ratio)})`}
              </span>
            ) : null}
          </>
        )}
      />

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
          {unscheduled.length} {unscheduled.length > 1 ? config.plural : config.noun}{" "}
          {unscheduled.length > 1 ? "have" : "has"} no start or due date and{" "}
          {unscheduled.length > 1 ? "aren't" : "isn't"} shown above:{" "}
          {formatUnscheduledNames(unscheduled.map((b) => b.name))}
        </p>
      ) : null}
    </div>
  );
}
