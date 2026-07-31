"use client";

import { useRouter } from "next/navigation";
import { Bar, BarChart, CartesianGrid, Cell, ReferenceLine, XAxis, YAxis } from "recharts";
import { ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart";
import {
  computeAxis,
  formatAxisTick,
  todayOffset,
  type DateRange,
  type GanttRow,
  type TaskScheduleState,
} from "@/lib/timeline";

/** ROW_HEIGHT must match the task-name column in TaskTimelineSection — the two
 *  are separate elements laid side by side, and only equal row heights keep a
 *  name aligned with its bar. AXIS_HEIGHT is reserved for the date axis so the
 *  labels are inside the container rather than clipped by it. */
export const ROW_HEIGHT = 44;
export const AXIS_HEIGHT = 28;
const BAR_SIZE = 20;

/** A bar's colour is a status, not a series identity: open work carries the
 *  brand hue, overdue work the destructive hue, and closed work recedes to
 *  muted so what's left to do is what stands out. Because it is status, hue
 *  never carries the meaning alone — the legend, the tooltip and the row's
 *  own status text all repeat it. */
const STATE_COLOR: Record<TaskScheduleState, string> = {
  open: "var(--chart-1)",
  overdue: "var(--destructive)",
  closed: "var(--muted-foreground)",
};

export const STATE_LABEL: Record<TaskScheduleState, string> = {
  open: "Open",
  overdue: "Overdue",
  closed: "Closed",
};

const chartConfig = {
  duration: { label: "Scheduled" },
} satisfies ChartConfig;

function formatDay(date: Date) {
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/** GanttTooltip replaces ChartTooltipContent, which renders one numeric value
 *  per series: a Gantt row's useful payload is a date *range* plus a status,
 *  not the raw millisecond offsets the stack is built from. The surface styling
 *  is kept identical to the shadcn tooltip so it sits in the same visual family
 *  as the rest of the chart. */
function GanttTooltip({
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
      <div className="text-muted-foreground tabular-nums">
        {row.start.getTime() === row.end.getTime()
          ? formatDay(row.start)
          : `${formatDay(row.start)} – ${formatDay(row.end)}`}
      </div>
    </div>
  );
}

/**
 * TaskGanttChart draws the bars of the Timeline view mode. It renders only the
 * plot: the task names live in a sibling column in TaskTimelineSection so each
 * one stays a real <Link> to the task's single view (SVG axis ticks would break
 * both that navigation and keyboard access).
 */
export function TaskGanttChart({
  rows,
  bounds,
  now,
}: {
  rows: GanttRow[];
  bounds: DateRange;
  now: Date;
}) {
  const router = useRouter();
  const axis = computeAxis(bounds);
  const total = bounds.end.getTime() - bounds.start.getTime();
  const today = todayOffset(bounds, now);

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
        barCategoryGap={0}
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
            carries no meaning of its own, so it is transparent and unlabelled. */}
        <Bar dataKey="offset" stackId="schedule" fill="transparent" isAnimationActive={false} />
        <Bar
          dataKey="duration"
          stackId="schedule"
          barSize={BAR_SIZE}
          radius={4}
          isAnimationActive={false}
          className="cursor-pointer"
          onClick={(data: unknown) => {
            const row = (data as { payload?: GanttRow })?.payload;
            if (row) router.push(`/tasks/${row.id}`);
          }}
        >
          {rows.map((row) => (
            <Cell key={row.id} fill={STATE_COLOR[row.state]} />
          ))}
        </Bar>
      </BarChart>
    </ChartContainer>
  );
}
