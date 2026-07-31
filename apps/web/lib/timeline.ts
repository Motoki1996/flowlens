// Pure date-math for the project Gantt/timeline view (issue #33). Kept
// separate from the component so the layout math is unit-testable without
// rendering a chart.
//
// Everything the chart consumes is expressed as **milliseconds offset from
// bounds.start**, not as absolute timestamps: the Gantt is drawn as a stacked
// horizontal bar chart (a transparent `offset` segment followed by a visible
// `duration` segment), and a stack has to accumulate from zero.

import type { Task } from "@/types";

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

export interface DateRange {
  start: Date;
  end: Date;
}

/** startOfDay truncates a date to local midnight, the granularity a Gantt row is drawn at. */
export function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function addDays(date: Date, days: number): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + days);
}

/**
 * effectiveRange returns a task's schedule range: startDate and dueOn when
 * both are set, or either one alone treated as a single-day range. Returns
 * null for a task with neither set — it has nothing to plot.
 */
export function effectiveRange(task: Pick<Task, "startDate" | "dueOn">): DateRange | null {
  const start = task.startDate ? new Date(task.startDate) : null;
  const end = task.dueOn ? new Date(task.dueOn) : null;
  if (!start && !end) return null;
  const s = start ?? end!;
  const e = end ?? start!;
  return s.getTime() <= e.getTime() ? { start: s, end: e } : { start: e, end: s };
}

/** hasSchedule reports whether a task has a startDate or dueOn to plot. */
export function hasSchedule(task: Pick<Task, "startDate" | "dueOn">): boolean {
  return effectiveRange(task) !== null;
}

/**
 * computeTimelineBounds returns the date span covering every scheduled task,
 * snapped to day boundaries and padded by one day on each side so edge bars
 * aren't flush against the axis. The end is exclusive: a task due on the last
 * day occupies the whole of that day rather than ending at its midnight.
 * Returns null when no task in tasks has a schedule.
 */
export function computeTimelineBounds(tasks: Pick<Task, "startDate" | "dueOn">[]): DateRange | null {
  const ranges = tasks.map(effectiveRange).filter((r): r is DateRange => r !== null);
  if (ranges.length === 0) return null;

  const earliest = new Date(Math.min(...ranges.map((r) => r.start.getTime())));
  const latest = new Date(Math.max(...ranges.map((r) => r.end.getTime())));
  return { start: addDays(startOfDay(earliest), -1), end: addDays(startOfDay(latest), 2) };
}

/** spanDays is the number of whole days bounds covers, at least 1. */
export function spanDays(bounds: DateRange): number {
  return Math.max(1, Math.round((bounds.end.getTime() - bounds.start.getTime()) / ONE_DAY_MS));
}

export type TickGranularity = "day" | "week" | "month";

export interface TimelineAxis {
  granularity: TickGranularity;
  /** Tick positions, in ms offset from bounds.start. */
  ticks: number[];
}

/**
 * computeAxis picks a tick interval that keeps the date axis readable at any
 * span — every day for a couple of weeks, then weekly, then the first of each
 * month — and returns the tick positions as offsets from bounds.start. Week
 * and month ticks are snapped to real week/month starts rather than to
 * multiples of the range, so the labels are dates a reader recognises.
 */
export function computeAxis(bounds: DateRange): TimelineAxis {
  const days = spanDays(bounds);
  const granularity: TickGranularity = days <= 21 ? "day" : days <= 120 ? "week" : "month";
  const offset = (d: Date) => d.getTime() - bounds.start.getTime();
  const ticks: number[] = [];

  if (granularity === "month") {
    const cursor = new Date(bounds.start.getFullYear(), bounds.start.getMonth(), 1);
    if (cursor.getTime() < bounds.start.getTime()) cursor.setMonth(cursor.getMonth() + 1);
    while (cursor.getTime() < bounds.end.getTime()) {
      ticks.push(offset(cursor));
      cursor.setMonth(cursor.getMonth() + 1);
    }
    return { granularity, ticks };
  }

  const step = granularity === "day" ? 1 : 7;
  // Weekly ticks start on the first Monday at or after bounds.start.
  const first =
    granularity === "day" ? bounds.start : addDays(bounds.start, (8 - bounds.start.getDay()) % 7);
  for (let d = first; d.getTime() < bounds.end.getTime(); d = addDays(d, step)) {
    ticks.push(offset(d));
  }
  return { granularity, ticks };
}

/** formatAxisTick renders a tick offset as the date label its granularity calls for. */
export function formatAxisTick(
  offsetMs: number,
  bounds: DateRange,
  granularity: TickGranularity,
): string {
  const date = new Date(bounds.start.getTime() + offsetMs);
  return granularity === "month"
    ? date.toLocaleDateString(undefined, { year: "numeric", month: "short" })
    : date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/**
 * TaskScheduleState is what colours a bar. It is a status, not an identity:
 * "overdue" is an open task whose due date has already passed, and "closed"
 * deliberately recedes so remaining work is what stands out.
 */
export type TaskScheduleState = "open" | "overdue" | "closed";

export interface GanttRow {
  id: string;
  title: string;
  state: TaskScheduleState;
  /** Transparent leading segment of the stacked bar, in ms from bounds.start. */
  offset: number;
  /** Visible segment, in ms. Always at least one whole day. */
  duration: number;
  /** Inclusive display dates, for labels and the tooltip. */
  start: Date;
  end: Date;
}

/**
 * toGanttRows converts scheduled tasks into stacked-bar rows, ordered by start
 * date so the chart reads top-left to bottom-right. Tasks with no schedule are
 * dropped — the caller lists them separately rather than inventing dates.
 */
export function toGanttRows(tasks: Task[], bounds: DateRange, now: Date): GanttRow[] {
  return tasks
    .map((task) => {
      const range = effectiveRange(task);
      if (!range) return null;
      const start = startOfDay(range.start);
      const endExclusive = addDays(startOfDay(range.end), 1);
      const state: TaskScheduleState =
        task.status === "closed"
          ? "closed"
          : task.dueOn && startOfDay(new Date(task.dueOn)).getTime() < startOfDay(now).getTime()
            ? "overdue"
            : "open";
      return {
        id: task.id,
        title: task.title,
        state,
        offset: start.getTime() - bounds.start.getTime(),
        duration: endExclusive.getTime() - start.getTime(),
        start,
        end: startOfDay(range.end),
      };
    })
    .filter((row): row is GanttRow => row !== null)
    .sort((a, b) => a.offset - b.offset || a.title.localeCompare(b.title));
}

/**
 * todayOffset locates "now" on the axis, or null when today falls outside the
 * plotted range — in which case the chart draws no marker rather than pinning
 * one to an edge, which would read as a real date.
 */
export function todayOffset(bounds: DateRange, now: Date): number | null {
  const today = startOfDay(now);
  if (today.getTime() < bounds.start.getTime() || today.getTime() >= bounds.end.getTime()) {
    return null;
  }
  return today.getTime() - bounds.start.getTime();
}
