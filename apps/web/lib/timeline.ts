// Pure date-math for the project Gantt/timeline view (issue #33). Kept
// separate from the component so the layout math is unit-testable without
// rendering a chart.
//
// Everything the chart consumes is expressed as **milliseconds offset from
// bounds.start**, not as absolute timestamps: the Gantt is drawn as a stacked
// horizontal bar chart (a transparent `offset` segment followed by a visible
// `duration` segment), and a stack has to accumulate from zero.

import type { Backlog, Priority, Progress, Task } from "@/types";

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

export interface DateRange {
  start: Date;
  end: Date;
}

/**
 * Scheduled is the shape the date math needs, and the only thing tasks and
 * backlogs have to share to be plottable: both carry the same optional
 * startDate/dueOn pair.
 */
export interface Scheduled {
  startDate: string | null;
  dueOn: string | null;
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
export function effectiveRange(task: Scheduled): DateRange | null {
  const start = task.startDate ? new Date(task.startDate) : null;
  const end = task.dueOn ? new Date(task.dueOn) : null;
  if (!start && !end) return null;
  const s = start ?? end!;
  const e = end ?? start!;
  return s.getTime() <= e.getTime() ? { start: s, end: e } : { start: e, end: s };
}

/** hasSchedule reports whether a task has a startDate or dueOn to plot. */
export function hasSchedule(task: Scheduled): boolean {
  return effectiveRange(task) !== null;
}

/**
 * computeTimelineBounds returns the date span covering every scheduled task,
 * snapped to day boundaries and padded by one day on each side so edge bars
 * aren't flush against the axis. The end is exclusive: a task due on the last
 * day occupies the whole of that day rather than ending at its midnight.
 * Returns null when no task in tasks has a schedule.
 */
export function computeTimelineBounds(tasks: Scheduled[]): DateRange | null {
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
 * TimelineZoom is how much horizontal room one day gets, named after the tick
 * interval that room affords. It is the reader's control over detail, and is
 * deliberately independent of `bounds`: the plotted range always covers every
 * scheduled object (the chart never hides data), and zoom decides whether that
 * range is skimmed at a glance or scrolled through day by day.
 */
export type TimelineZoom = "day" | "week" | "month";

export const TIMELINE_ZOOMS: TimelineZoom[] = ["month", "week", "day"];

/** dayWidth is in px; the axis granularity follows from it rather than from the
 *  span, so zooming in on a year-long project really does yield daily ticks. */
export const ZOOM_LEVELS: Record<TimelineZoom, { label: string; dayWidth: number }> = {
  day: { label: "Day", dayWidth: 28 },
  week: { label: "Week", dayWidth: 10 },
  month: { label: "Month", dayWidth: 4 },
};

/** The plot never draws narrower than this, so a one-week project still gets a
 *  readable axis instead of a stub. */
export const MIN_PLOT_WIDTH = 480;

/**
 * defaultZoom is the level a timeline opens at: the one whose ticks the span
 * calls for, so a two-week sprint lands on daily ticks and a year-long plan on
 * monthly ones without anybody touching the control. Reading the *data* for the
 * initial value and then letting the reader override it is the whole design —
 * the thresholds match what the axis used to derive on its own.
 */
export function defaultZoom(bounds: DateRange): TimelineZoom {
  const days = spanDays(bounds);
  return days <= 21 ? "day" : days <= 120 ? "week" : "month";
}

/** plotWidth is how wide the bars area is drawn at a zoom level. Anything past
 *  the container scrolls horizontally rather than compressing the bars. */
export function plotWidth(bounds: DateRange, zoom: TimelineZoom): number {
  return Math.max(MIN_PLOT_WIDTH, spanDays(bounds) * ZOOM_LEVELS[zoom].dayWidth);
}

/**
 * computeAxis returns the tick positions as offsets from bounds.start. Week and
 * month ticks are snapped to real week/month starts rather than to multiples of
 * the range, so the labels are dates a reader recognises.
 *
 * With no zoom given it picks the interval the span calls for — every day for a
 * couple of weeks, then weekly, then the first of each month. Passing a zoom
 * hands that choice to the reader instead.
 */
export function computeAxis(bounds: DateRange, zoom?: TimelineZoom): TimelineAxis {
  // A zoom level is named after the tick interval it affords, so it *is* the
  // granularity — no mapping table sits between the two.
  const granularity: TickGranularity = zoom ?? defaultZoom(bounds);
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
 * ScheduleState is what colours a bar. It is a status, not an identity:
 * "overdue" is unfinished work whose due date has already passed, and "closed"
 * deliberately recedes so remaining work is what stands out. Backlog bars use
 * the same three values, read off their tasks rather than off a status column.
 */
export type ScheduleState = "open" | "overdue" | "closed";

/** TaskCompletion is the closed/total task ratio a backlog bar is filled by.
 *  Deliberately not called "progress": that name belongs to the four-stage work
 *  state a backlog carries itself (Progress in @/types), which this ratio is
 *  independent of — a backlog can read In progress with nothing closed yet. */
export interface TaskCompletion {
  closed: number;
  total: number;
  /** closed / total, 0 for a backlog with no tasks. */
  ratio: number;
}

export interface GanttRow {
  id: string;
  title: string;
  priority: Priority;
  progress: Progress;
  state: ScheduleState;
  /** Transparent leading segment of the stacked bar, in ms from bounds.start. */
  offset: number;
  /** Visible segment, in ms. Always at least one whole day. */
  duration: number;
  /** Inclusive display dates, for labels and the tooltip. */
  start: Date;
  end: Date;
  /** Closed/total task ratio, set on backlog rows only; task rows are closed
   *  or not, never partial. Separate from `progress` above. */
  completion?: TaskCompletion;
}

/** isOverdue reports whether dueOn is a day already behind us. */
function isOverdue(dueOn: string | null, now: Date): boolean {
  return !!dueOn && startOfDay(new Date(dueOn)).getTime() < startOfDay(now).getTime();
}

/** buildRow lays one scheduled object out on the axis, or returns null when it
 *  has no dates to plot. Everything object-specific arrives already decided. */
function buildRow(
  item: { id: string; title: string; priority: Priority; progress: Progress } & Scheduled,
  bounds: DateRange,
  state: ScheduleState,
  completion?: TaskCompletion,
): GanttRow | null {
  const range = effectiveRange(item);
  if (!range) return null;
  const start = startOfDay(range.start);
  const endExclusive = addDays(startOfDay(range.end), 1);
  return {
    id: item.id,
    title: item.title,
    priority: item.priority,
    progress: item.progress,
    state,
    offset: start.getTime() - bounds.start.getTime(),
    duration: endExclusive.getTime() - start.getTime(),
    start,
    end: startOfDay(range.end),
    ...(completion ? { completion } : {}),
  };
}

/** byStartThenTitle orders rows so the chart reads top-left to bottom-right. */
function byStartThenTitle(a: GanttRow, b: GanttRow): number {
  return a.offset - b.offset || a.title.localeCompare(b.title);
}

/**
 * toTaskGanttRows converts scheduled tasks into stacked-bar rows. Tasks with no
 * schedule are dropped — the caller lists them separately rather than inventing
 * dates.
 */
export function toTaskGanttRows(tasks: Task[], bounds: DateRange, now: Date): GanttRow[] {
  return tasks
    .map((task) =>
      buildRow(
        task,
        bounds,
        task.status === "closed" ? "closed" : isOverdue(task.dueOn, now) ? "overdue" : "open",
      ),
    )
    .filter((row): row is GanttRow => row !== null)
    .sort(byStartThenTitle);
}

/**
 * backlogCompletion counts how much of a backlog is done. A backlog with no tasks
 * reports 0/0 at ratio 0 rather than "complete": an empty backlog has not been
 * finished, and filling its bar would say it had.
 */
export function backlogCompletion(tasks: Task[], backlogId: string): TaskCompletion {
  const owned = tasks.filter((t) => t.backlogId === backlogId);
  const closed = owned.filter((t) => t.status === "closed").length;
  return {
    closed,
    total: owned.length,
    ratio: owned.length === 0 ? 0 : closed / owned.length,
  };
}

/**
 * toBacklogGanttRows converts scheduled backlogs into stacked-bar rows carrying
 * their completion. A backlog's state comes from its tasks, not from a status
 * column it doesn't have: it is "closed" once every task in it is closed, and
 * "overdue" while unfinished work sits past its due date.
 */
export function toBacklogGanttRows(
  backlogs: Backlog[],
  tasks: Task[],
  bounds: DateRange,
  now: Date,
): GanttRow[] {
  return backlogs
    .map((backlog) => {
      const progress = backlogCompletion(tasks, backlog.id);
      const complete = progress.total > 0 && progress.closed === progress.total;
      const state: ScheduleState = complete
        ? "closed"
        : isOverdue(backlog.dueOn, now)
          ? "overdue"
          : "open";
      return buildRow({ ...backlog, title: backlog.name }, bounds, state, progress);
    })
    .filter((row): row is GanttRow => row !== null)
    .sort(byStartThenTitle);
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
