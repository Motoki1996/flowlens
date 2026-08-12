"use client";

import { useRouter } from "next/navigation";
import { Bar, BarChart, CartesianGrid, Cell, ReferenceLine, XAxis, YAxis } from "recharts";
import { ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";
import { PRIORITY_LABELS } from "@/lib/priority";
import { PROGRESS_LABELS } from "@/lib/progress";
import {
  computeAxis,
  formatAxisTick,
  todayOffset,
  type DateRange,
  type GanttRow,
  type ScheduleState,
  type TimelineZoom,
} from "@/lib/timeline";

/** ROW_HEIGHT must match the name column of the section wrapping this chart —
 *  the two are separate elements laid side by side, and only equal row heights
 *  keep a name aligned with its bar. AXIS_HEIGHT is reserved for the date axis
 *  so the labels are inside the container rather than clipped by it. */
export const ROW_HEIGHT = 44;
export const AXIS_HEIGHT = 28;

/** NAME_COLUMN_CLASS sizes that same sibling column, shared so the Task and the
 *  Backlog timeline read as one chart. Width is Tailwind classes rather than a
 *  pixel constant because — unlike the row height, which must match exactly for
 *  a name to line up with its bar — width only has to be identical between the
 *  two sections: a narrow viewport can hand the plot more room, and a wide one
 *  gives titles enough space not to be truncated on sight. */
export const NAME_COLUMN_CLASS = "w-40 shrink-0 sm:w-64 lg:w-72";
/** A bar is deliberately shorter than its row: the leftover height is the gap
 *  that keeps neighbouring bars from reading as one block. */
const BAR_SIZE = 20;

/** A bar's colour is a status, not a series identity: open work carries the
 *  brand hue, overdue work the destructive hue, and closed work recedes to
 *  muted so what's left to do is what stands out. Because it is status, hue
 *  never carries the meaning alone — the legend, the tooltip and the row's
 *  own status text all repeat it. */
const STATE_COLOR: Record<ScheduleState, string> = {
  open: "var(--chart-1)",
  overdue: "var(--destructive)",
  closed: "var(--muted-foreground)",
};

export const STATE_LABEL: Record<ScheduleState, string> = {
  open: "Open",
  overdue: "Overdue",
  closed: "Closed",
};

/** The unfinished part of a backlog bar keeps the row's own colour rather than
 *  taking a second hue: the split is "how much of this bar is done", which is
 *  one measure, not two categories. */
const REMAINING_OPACITY = 0.25;

const chartConfig = {
  duration: { label: "Scheduled" },
} satisfies ChartConfig;

function formatDay(date: Date) {
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/** percent renders a completion ratio, so every place that states one — the
 *  tooltip here and the name column beside the chart — rounds it identically. */
export function percent(ratio: number): string {
  return `${Math.round(ratio * 100)}%`;
}

/** GanttTooltip replaces ChartTooltipContent, which renders one numeric value
 *  per series: a Gantt row's useful payload is a date *range* plus a status,
 *  not the raw millisecond offsets the stack is built from. The surface styling
 *  is kept identical to the shadcn tooltip so it sits in the same visual family
 *  as the rest of the chart. Exported only so it can be asserted on directly:
 *  recharts decides when a tooltip mounts from pointer geometry an SVG doesn't
 *  have in jsdom. */
export function GanttTooltip({
  active,
  payload,
}: {
  active?: boolean;
  payload?: { payload: GanttRow }[];
}) {
  if (!active || !payload?.length) return null;
  const row = payload[0].payload;

  return (
    <div className="border-border/50 bg-background grid min-w-[10rem] gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs shadow-xl">
      <div className="text-foreground font-medium">{row.title}</div>
      <div className="text-muted-foreground flex items-center gap-1.5">
        <span
          className="size-2 shrink-0 rounded-[2px]"
          style={{ backgroundColor: STATE_COLOR[row.state] }}
        />
        {STATE_LABEL[row.state]}
      </div>
      {/* Priority and progress live here rather than in the name column beside
          the chart: two pills per row crowded the title down to an ellipsis,
          and neither field is what a timeline is read for. The name column
          still flags a high or urgent priority, so nothing that changes what
          you look at first is hover-only. */}
      <div className="text-muted-foreground">
        {PRIORITY_LABELS[row.priority]} priority · {PROGRESS_LABELS[row.progress]}
      </div>
      <div className="text-muted-foreground tabular-nums">
        {row.start.getTime() === row.end.getTime()
          ? formatDay(row.start)
          : `${formatDay(row.start)} – ${formatDay(row.end)}`}
      </div>
      {/* Naming the counts keeps the fill from being the only statement of
          progress: "50%" and "1/2 tasks closed" carry different confidence. */}
      {row.completion ? (
        <div className="text-muted-foreground tabular-nums">
          {row.completion.total === 0
            ? "No tasks"
            : `${row.completion.closed}/${row.completion.total} tasks closed (${percent(row.completion.ratio)})`}
        </div>
      ) : null}
    </div>
  );
}

/** doneWidth / remainingWidth split a bar at its completion ratio. A row with
 *  no progress (a task) is all "done" and draws as one solid bar, since a task
 *  is closed or it isn't — its colour already says which. */
const doneWidth = (row: GanttRow) =>
  row.completion ? row.duration * row.completion.ratio : row.duration;
const remainingWidth = (row: GanttRow) =>
  row.completion ? row.duration * (1 - row.completion.ratio) : 0;

/**
 * GanttChart draws the bars of a Timeline view mode. It renders only the plot:
 * the row names live in a sibling column in the section that wraps it, so each
 * one stays a real <Link> to that object's single view (SVG axis ticks would
 * break both that navigation and keyboard access). The completion percentage
 * is stated in that same column rather than inside the SVG, for the same
 * reason — text in the chart is neither selectable nor reliably announced.
 */
export function GanttChart({
  rows,
  bounds,
  now,
  href,
  zoom,
}: {
  rows: GanttRow[];
  bounds: DateRange;
  now: Date;
  /** Where clicking a bar goes — the single view of whatever the row is. */
  href: (row: GanttRow) => string;
  /** Tick interval for the date axis. Omitted, the axis derives one from the
   *  span — the chart stays usable on its own, without a zoom control. */
  zoom?: TimelineZoom;
}) {
  const router = useRouter();
  const axis = computeAxis(bounds, zoom);
  const total = bounds.end.getTime() - bounds.start.getTime();
  const today = todayOffset(bounds, now);
  const hasProgress = rows.some((row) => row.completion);

  const open = (data: unknown) => {
    const row = (data as { payload?: GanttRow })?.payload;
    if (row) router.push(href(row));
  };

  return (
    <ChartContainer
      config={chartConfig}
      className="aspect-auto w-full"
      style={{ height: rows.length * ROW_HEIGHT + AXIS_HEIGHT }}
    >
      <BarChart
        accessibilityLayer
        data={rows}
        layout="vertical"
        margin={{ top: 0, right: 8, bottom: 0, left: 0 }}
      >
        <CartesianGrid horizontal={false} />
        {/* The axis is a date scale, not a measure of any one series, so it
            carries no dataKey — the domain is the plotted range and the ticks
            are real calendar dates. */}
        <XAxis
          type="number"
          domain={[0, total]}
          ticks={axis.ticks}
          tickFormatter={(value: number) => formatAxisTick(value, bounds, axis.granularity)}
          orientation="top"
          height={AXIS_HEIGHT}
          axisLine={false}
          tickLine={false}
          minTickGap={16}
        />
        <YAxis type="category" dataKey="id" hide />
        <ChartTooltip cursor={{ fill: "var(--accent)", fillOpacity: 0.5 }} content={<GanttTooltip />} />
        {today !== null ? (
          <ReferenceLine
            x={today}
            stroke="var(--muted-foreground)"
            label={{
              value: "Today",
              position: "insideTopRight",
              fill: "var(--muted-foreground)",
              fontSize: 11,
            }}
          />
        ) : null}
        {/* The leading segment positions the visible bar at its start date; it
            carries no meaning of its own, so it is transparent and unlabelled.
            It still needs barSize: recharts sizes a whole stack from its *first*
            bar, so leaving this one unsized let every stack fill its row band
            edge to edge and the bars ran together vertically. */}
        <Bar
          dataKey="offset"
          stackId="schedule"
          barSize={BAR_SIZE}
          fill="transparent"
          isAnimationActive={false}
        />
        <Bar
          dataKey={doneWidth}
          name="done"
          stackId="schedule"
          barSize={BAR_SIZE}
          // A split bar is squared off where its two segments meet, so the
          // boundary between done and remaining reads as a straight edge.
          radius={hasProgress ? [4, 0, 0, 4] : 4}
          isAnimationActive={false}
          className="cursor-pointer"
          onClick={open}
        >
          {rows.map((row) => (
            <Cell key={row.id} fill={STATE_COLOR[row.state]} />
          ))}
        </Bar>
        {hasProgress ? (
          <Bar
            dataKey={remainingWidth}
            name="remaining"
            stackId="schedule"
            barSize={BAR_SIZE}
            radius={[0, 4, 4, 0]}
            isAnimationActive={false}
            className="cursor-pointer"
            onClick={open}
          >
            {rows.map((row) => (
              <Cell key={row.id} fill={STATE_COLOR[row.state]} fillOpacity={REMAINING_OPACITY} />
            ))}
          </Bar>
        ) : null}
      </BarChart>
    </ChartContainer>
  );
}
