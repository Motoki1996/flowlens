// Pure date-math for the project Gantt/timeline view (issue #33). Kept
// separate from the component so the layout math is unit-testable without
// rendering.

import type { Task } from "@/types";

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

export interface DateRange {
  start: Date;
  end: Date;
}

export interface BarStyle {
  leftPercent: number;
  widthPercent: number;
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
 * padded by one day on each side so edge bars aren't flush against the
 * chart's border. Returns null when no task in tasks has a schedule.
 */
export function computeTimelineBounds(tasks: Pick<Task, "startDate" | "dueOn">[]): DateRange | null {
  const ranges = tasks.map(effectiveRange).filter((r): r is DateRange => r !== null);
  if (ranges.length === 0) return null;

  const minStart = Math.min(...ranges.map((r) => r.start.getTime()));
  const maxEnd = Math.max(...ranges.map((r) => r.end.getTime()));
  const start = new Date(minStart - ONE_DAY_MS);
  const end = new Date(Math.max(maxEnd + ONE_DAY_MS, start.getTime() + ONE_DAY_MS));
  return { start, end };
}

/**
 * barStyle positions a task's bar within bounds as left/width percentages.
 * A single-day task still gets a visible sliver (MIN_WIDTH_PERCENT) rather
 * than a zero-width bar. Returns null when the task has no schedule or
 * bounds has zero span.
 */
export function barStyle(task: Pick<Task, "startDate" | "dueOn">, bounds: DateRange): BarStyle | null {
  const range = effectiveRange(task);
  if (!range) return null;
  const total = bounds.end.getTime() - bounds.start.getTime();
  if (total <= 0) return null;

  const MIN_WIDTH_PERCENT = 1.5;
  const left = ((range.start.getTime() - bounds.start.getTime()) / total) * 100;
  const width = ((range.end.getTime() - range.start.getTime()) / total) * 100;
  return {
    leftPercent: Math.min(Math.max(left, 0), 100),
    widthPercent: Math.max(width, MIN_WIDTH_PERCENT),
  };
}
