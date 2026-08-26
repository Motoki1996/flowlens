// Pure date-math for the project Gantt/timeline view (issue #33). Kept
// separate from the component so the layout math is unit-testable without
// rendering a chart.
//
// Everything the chart consumes is expressed as **milliseconds offset from
// bounds.start**, not as absolute timestamps: the Gantt is drawn as a stacked
// horizontal bar chart (a transparent `offset` segment followed by a visible
// `duration` segment), and a stack has to accumulate from zero.

import type { Priority, Progress, Task } from "@/types";
import type { Grouping } from "@/lib/groups";

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

const MAX_UNSCHEDULED_NAMES = 3;

/**
 * formatUnscheduledNames joins the names of items the timeline can't plot,
 * capped so a large backlog doesn't turn the "not shown above" note into an
 * unreadable wall of text — the rest are summarised as a count instead.
 */
export function formatUnscheduledNames(names: string[]): string {
  if (names.length <= MAX_UNSCHEDULED_NAMES) return names.join(", ");
  const shown = names.slice(0, MAX_UNSCHEDULED_NAMES);
  const hidden = names.length - shown.length;
  return `${shown.join(", ")}, and ${hidden} more`;
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

/** TickGranularity is a zoom level read as an interval. The two are one set of
 *  names on purpose — a level is named after the ticks it affords — so no
 *  mapping table can drift between them. */
export type TickGranularity = TimelineZoom;

export interface TimelineAxis {
  granularity: TickGranularity;
  /** Labelled tick positions, in ms offset from bounds.start. */
  ticks: number[];
  /**
   * Unlabelled gridlines subdividing those ticks, same units. A coarse zoom
   * labels a span too long to judge a bar against — "Aug 2026" says nothing
   * about which week a bar ends in — so the interval below it is still drawn,
   * fainter and without a label. Empty at the two finest zooms, where the
   * labels are already at the finest interval a calendar offers.
   */
  minorTicks: number[];
}

/**
 * TimelineZoom is how much horizontal room one day gets, named after the tick
 * interval that room affords. It is the reader's control over detail, and is
 * deliberately independent of `bounds`: the plotted range always covers every
 * scheduled object (the chart never hides data), and zoom decides whether that
 * range is skimmed at a glance or scrolled through day by day.
 */
export type TimelineZoom = "day" | "week" | "month" | "quarter";

export const TIMELINE_ZOOMS: TimelineZoom[] = ["quarter", "month", "week", "day"];

/** dayWidth is in px; the axis granularity follows from it rather than from the
 *  span, so zooming in on a year-long project really does yield daily ticks.
 *
 *  Quarter is deliberately the coarsest level. It is the unit a roadmap is
 *  actually planned in, and it is the last one at which a bar is still a bar:
 *  a year-wide level would draw a fortnight of work half a pixel wide, which
 *  is a heatmap, not a Gantt. Anything longer is read by scrolling. */
export const ZOOM_LEVELS: Record<TimelineZoom, { label: string; dayWidth: number }> = {
  day: { label: "Day", dayWidth: 28 },
  week: { label: "Week", dayWidth: 10 },
  month: { label: "Month", dayWidth: 4 },
  quarter: { label: "Quarter", dayWidth: 1.6 },
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
  return days <= 21 ? "day" : days <= 120 ? "week" : days <= 550 ? "month" : "quarter";
}

/** plotWidth is how wide the bars area is drawn at a zoom level. Anything past
 *  the container scrolls horizontally rather than compressing the bars. */
export function plotWidth(bounds: DateRange, zoom: TimelineZoom): number {
  return Math.max(MIN_PLOT_WIDTH, spanDays(bounds) * ZOOM_LEVELS[zoom].dayWidth);
}

/** The narrowest a bar may be drawn, whatever its duration works out to in px. */
export const MIN_BAR_PX = 6;

/**
 * minBarDuration is MIN_BAR_PX expressed in milliseconds at a zoom level, so a
 * short task stays visible at a coarse one: a single day is 1.6px wide at
 * quarter zoom, which is a smudge, and a bar nobody can see reads as a task
 * that isn't scheduled at all. It only ever widens a bar — at day and week
 * zoom a whole day is already past the floor, so nothing moves.
 */
export function minBarDuration(zoom: TimelineZoom): number {
  return (MIN_BAR_PX / ZOOM_LEVELS[zoom].dayWidth) * ONE_DAY_MS;
}

/** ticksFrom walks `first` forward by `next` and returns every step that lands
 *  inside bounds, as ms offsets from bounds.start. */
function ticksFrom(bounds: DateRange, first: Date, next: (d: Date) => Date): number[] {
  const ticks: number[] = [];
  for (let d = first; d.getTime() < bounds.end.getTime(); d = next(d)) {
    if (d.getTime() >= bounds.start.getTime()) ticks.push(d.getTime() - bounds.start.getTime());
  }
  return ticks;
}

/** monthTicks returns the first of every `step`th month in bounds — step 1 for
 *  months, 3 for quarters, which are snapped to January/April/July/October
 *  rather than to whichever month the range happens to open in. */
function monthTicks(bounds: DateRange, step: number): number[] {
  const month = bounds.start.getMonth();
  const first = new Date(bounds.start.getFullYear(), step === 3 ? month - (month % 3) : month, 1);
  if (first.getTime() < bounds.start.getTime()) first.setMonth(first.getMonth() + step);
  return ticksFrom(bounds, first, (d) => new Date(d.getFullYear(), d.getMonth() + step, 1));
}

/** weekTicks returns every Monday in bounds, starting at the first one at or
 *  after bounds.start. */
function weekTicks(bounds: DateRange): number[] {
  const first = addDays(bounds.start, (8 - bounds.start.getDay()) % 7);
  return ticksFrom(bounds, first, (d) => addDays(d, 7));
}

/** subdivide drops the minor ticks that a labelled tick already sits on — a
 *  month that starts on a Monday is one gridline, not two stacked ones, which
 *  would draw darker than its neighbours for no reason. */
function subdivide(ticks: number[], candidates: number[]): number[] {
  const labelled = new Set(ticks);
  return candidates.filter((t) => !labelled.has(t));
}

/**
 * computeAxis returns the tick positions as offsets from bounds.start, snapped
 * to real quarter/month/week starts rather than to multiples of the range, so
 * the labels are dates a reader recognises.
 *
 * The two coarse levels also return `minorTicks`, the interval one step finer:
 * a month label is too wide to place a bar against on its own, so the weeks
 * inside it are still drawn — unlabelled and fainter (see TimelineAxis).
 *
 * With no zoom given it picks the interval the span calls for — every day for a
 * couple of weeks, then weekly, then the first of each month. Passing a zoom
 * hands that choice to the reader instead.
 */
export function computeAxis(bounds: DateRange, zoom?: TimelineZoom): TimelineAxis {
  // A zoom level is named after the tick interval it affords, so it *is* the
  // granularity — no mapping table sits between the two.
  const granularity: TickGranularity = zoom ?? defaultZoom(bounds);

  switch (granularity) {
    case "quarter": {
      const ticks = monthTicks(bounds, 3);
      return { granularity, ticks, minorTicks: subdivide(ticks, monthTicks(bounds, 1)) };
    }
    case "month": {
      const ticks = monthTicks(bounds, 1);
      return { granularity, ticks, minorTicks: subdivide(ticks, weekTicks(bounds)) };
    }
    case "week":
      return { granularity, ticks: weekTicks(bounds), minorTicks: [] };
    default:
      return {
        granularity,
        ticks: ticksFrom(bounds, bounds.start, (d) => addDays(d, 1)),
        minorTicks: [],
      };
  }
}

/** formatAxisTick renders a tick offset as the date label its granularity calls for. */
export function formatAxisTick(
  offsetMs: number,
  bounds: DateRange,
  granularity: TickGranularity,
): string {
  const date = new Date(bounds.start.getTime() + offsetMs);
  // Intl has no quarter field, so that one label is composed by hand.
  if (granularity === "quarter") {
    return `Q${Math.floor(date.getMonth() / 3) + 1} ${date.getFullYear()}`;
  }
  return granularity === "month"
    ? date.toLocaleDateString(undefined, { year: "numeric", month: "short" })
    : date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** AxisBand is a shaded span of the axis, in ms offsets from bounds.start —
 *  the same units as a tick, so the chart places both the same way. */
export interface AxisBand {
  start: number;
  end: number;
}

/**
 * weekendBands returns Saturday→Monday spans across bounds, clipped to it. The
 * chart shades them at day zoom, which is where the question a Gantt gets asked
 * is "how many working days is that?" — a bar crossing a weekend is shorter
 * than its width claims, and nothing else on the axis says so.
 */
export function weekendBands(bounds: DateRange): AxisBand[] {
  const total = bounds.end.getTime() - bounds.start.getTime();
  // Start from the Saturday at or before bounds.start, so a range that opens
  // mid-weekend still gets its remaining shaded day.
  const first = addDays(bounds.start, -((bounds.start.getDay() + 1) % 7));
  const bands: AxisBand[] = [];
  for (let d = first; d.getTime() < bounds.end.getTime(); d = addDays(d, 7)) {
    const start = Math.max(0, d.getTime() - bounds.start.getTime());
    const end = Math.min(total, addDays(d, 2).getTime() - bounds.start.getTime());
    if (end > start) bands.push({ start, end });
  }
  return bands;
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
 * groupTaskCompletion reads a backlog's or epic's own taskCount/closedTaskCount,
 * aggregated server-side (issue #144), rather than filtering a full task
 * list the way backlogCompletion does. The Backlog collection's Board and
 * Timeline modes use this so they don't need to fetch every task in the
 * project just to show a ratio; the Backlog single view still has its own
 * (already backlog-scoped) task list and uses backlogCompletion instead.
 */
export function groupTaskCompletion(backlog: {
  taskCount: number;
  closedTaskCount: number;
}): TaskCompletion {
  return {
    closed: backlog.closedTaskCount,
    total: backlog.taskCount,
    ratio: backlog.taskCount === 0 ? 0 : backlog.closedTaskCount / backlog.taskCount,
  };
}

/**
 * toGroupGanttRows converts scheduled backlogs or epics into stacked-bar rows
 * carrying their completion, read off each one's own taskCount/closedTaskCount
 * (see backlogTaskCompletion). A backlog's state comes from that ratio, not
 * from a status column it doesn't have: it is "closed" once every task in it
 * is closed, and "overdue" while unfinished work sits past its due date.
 */
export function toGroupGanttRows(backlogs: Grouping[], bounds: DateRange, now: Date): GanttRow[] {
  return backlogs
    .map((backlog) => {
      const progress = backlogTaskCompletion(backlog);
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

/** backlogTaskCompletion and toBacklogGanttRows are the two group helpers
 *  under their original names, kept because the Backlog screens read better
 *  saying "backlog". */
export const backlogTaskCompletion = groupTaskCompletion;
export const toBacklogGanttRows = toGroupGanttRows;
