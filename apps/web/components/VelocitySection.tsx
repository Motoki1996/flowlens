"use client";

import { useState } from "react";
import {
  Bar,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  XAxis,
  YAxis,
  type LegendPayload,
} from "recharts";
import type { MetricsInterval, Velocity, VelocityPeriod } from "@/types";
import { periodLabel } from "@/lib/dates";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { MetricTabs } from "@/components/MetricTabs";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";

/** The three actor buckets a completed task falls into (issue #195), stacked
 *  bottom-to-top in this order — see app/globals.css's --chart-N assignment
 *  comment. Reused from slots 1-3 (already used by DeliveryMetricsSection's
 *  own, separate stage chart; a chart's colors only need to be internally
 *  consistent, not globally unique — see GanttChart/TaskTimelineSection's own
 *  reuse of slot 1). */
const velocityChartConfig = {
  completedByUser: { label: "User", color: "var(--chart-1)" },
  completedByAgent: { label: "Agent", color: "var(--chart-2)" },
  completedByUnknown: { label: "Unknown", color: "var(--chart-3)" },
  movingAverage: { label: "Moving average", color: "var(--chart-4)" },
} satisfies ChartConfig;

/** recharts 3's <Legend> defaults to alphabetical order, which would put
 *  "Agent" before "Moving average" before "Unknown" before "User" — scrambling
 *  the stack order and inviting the same mismatch issue #187 already fixed
 *  once for DeliveryMetricsSection. Sort by position in velocityChartConfig
 *  instead, so the legend always reads in stack order. */
const velocityKeys = Object.keys(velocityChartConfig);
function velocityLegendItemSorter(item: LegendPayload) {
  return velocityKeys.indexOf(String(item.dataKey));
}

/** A still-running period (`complete: false`, typically the current week)
 *  reads low by construction — it hasn't finished yet — so its bars are
 *  drawn at reduced opacity rather than at face value, which would otherwise
 *  misread as a slowdown. */
const INCOMPLETE_PERIOD_OPACITY = 0.35;

function barOpacity(entry: { complete: boolean }): number {
  return entry.complete ? 1 : INCOMPLETE_PERIOD_OPACITY;
}

/** Unit switches the whole card between counting tasks and counting
 *  size-weighted points. Both come from the same response — the server has
 *  already applied the weights (apps/api/internal/velocity), so this never
 *  multiplies anything itself. */
type Unit = "tasks" | "points";

const UNIT_TABS: ReadonlyArray<{ key: Unit; label: string }> = [
  { key: "tasks", label: "Tasks" },
  { key: "points", label: "Points" },
];

const UNIT_NOUN: Record<Unit, string> = { tasks: "tasks", points: "points" };

/** formatVelocity renders averageVelocity as "9.5 tasks/week" — or a
 *  placeholder once there is no complete period to average yet, which is
 *  different from a velocity of zero and must not be shown as a number. */
function formatVelocity(averageVelocity: number | null, interval: MetricsInterval, unit: Unit): string {
  if (averageVelocity == null) return "Not enough completed tasks yet";
  return `${averageVelocity.toFixed(1)} ${UNIT_NOUN[unit]}/${interval}`;
}

/** formatForecast renders the forecast alongside what is left: "34 open ≈ 3.6
 *  weeks left". null whenever the matching average is null or zero, in which
 *  case no forecast can honestly be made. */
function formatForecast(
  forecastPeriods: number | null,
  openTotal: number,
  interval: MetricsInterval,
  unit: Unit,
): string {
  const remaining = `${openTotal} ${UNIT_NOUN[unit]} open`;
  if (forecastPeriods == null) return `${remaining} — no forecast yet`;
  return `${remaining} ≈ ${forecastPeriods.toFixed(1)} ${interval}s left`;
}

const PERIOD_CHART_HEIGHT = 220;

/** Left to itself recharts widens a bar to fill its whole category band, so a
 *  range holding only a handful of periods draws slabs wide enough to read as
 *  a block diagram rather than a chart. Cap the width and widen the gap
 *  between categories instead, so the bars stay slim at any period count. */
const BAR_MAX_SIZE = 28;
const BAR_CATEGORY_GAP = "35%";

/**
 * VelocitySection is the Project single view's completed-task throughput
 * chart (issue #196, following #195's API), placed immediately before
 * DeliveryMetricsSection so it reads velocity -> lead time in that order:
 * velocity alone is a misleading number (task-splitting can inflate it for
 * free), and only makes sense read alongside lead time — "throughput is up
 * and lead time hasn't gotten worse" is the only combination worth trusting.
 * There is deliberately no standalone /velocity screen.
 *
 * It shares DeliveryMetricsSection's from/to/interval URL filter rather than
 * exposing its own selector — both sections read the same
 * ?from=&to=&interval= query params, set by DeliveryMetricsSection's own
 * controls. Unlike DeliveryMetricsSection's interval (which defaults to "no
 * bucketing" when omitted from the URL), the velocity API always buckets
 * (defaulting to "week" server-side) — periods are the metric here, not an
 * optional add-on — so this chart always draws one bar per period.
 *
 * The Tasks/Points tab switches which of the two series the whole card reads
 * (bars, moving average and both stats together, never a mix). Points weight
 * each task by its size, so they answer "how much work finished" where the
 * raw count answers "how many items finished" — a count can be inflated for
 * free by splitting tasks smaller. Both arrive pre-weighted from the API.
 * When no completed task in range has been given a size, the point series is
 * necessarily a flat 3x copy of the counts (every task defaults to size M),
 * and the card says so rather than passing a rescaled duplicate off as a
 * second opinion.
 */
export function VelocitySection({
  velocity,
  error = false,
}: {
  velocity: Velocity | null;
  error?: boolean;
}) {
  const [unit, setUnit] = useState<Unit>("tasks");

  const periods: VelocityPeriod[] = velocity?.periods ?? [];
  const hasData = periods.length > 0;
  const interval = velocity?.interval ?? "week";
  const points = unit === "points";

  const chartData = periods.map((period) => ({
    row: periodLabel(period, interval),
    completedByUser: points ? period.completedPointsByUser : period.completedByUser,
    completedByAgent: points ? period.completedPointsByAgent : period.completedByAgent,
    completedByUnknown: points ? period.completedPointsByUnknown : period.completedByUnknown,
    movingAverage: points ? period.movingAveragePoints : period.movingAverage,
    complete: period.complete,
  }));

  const averageVelocity = points ? velocity?.averageVelocityPoints ?? null : velocity?.averageVelocity ?? null;
  const forecastPeriods = points ? velocity?.forecastPeriodsByPoints ?? null : velocity?.forecastPeriods ?? null;
  const openTotal = (points ? velocity?.openTaskPoints : velocity?.openTaskCount) ?? 0;

  // Every task starts at size M, so before anyone sizes anything the points
  // series carries no information the task count doesn't. Saying so is the
  // difference between an honest chart and one that implies a second
  // measurement it doesn't have.
  const nothingSized = !!velocity && velocity.sizedTaskRatio === 0;

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Velocity</CardTitle>
          {hasData && !error ? (
            <MetricTabs label="Unit" tabs={UNIT_TABS} value={unit} onChange={setUnit} />
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-6">
        {error ? (
          <p className="text-destructive text-sm">Failed to load velocity.</p>
        ) : !hasData ? (
          <p className="text-muted-foreground text-sm">No completed tasks yet.</p>
        ) : (
          <>
            <dl className="grid grid-cols-2 gap-x-8 gap-y-3 text-sm">
              <div>
                <dt className="text-muted-foreground">Average velocity (last complete periods)</dt>
                <dd className="text-foreground font-medium">
                  {formatVelocity(averageVelocity, interval, unit)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Forecast</dt>
                <dd className="text-foreground font-medium">
                  {formatForecast(forecastPeriods, openTotal, interval, unit)}
                </dd>
              </div>
            </dl>

            {points && nothingSized ? (
              <p className="text-muted-foreground text-xs">
                No completed task in this range has been given a size yet, so points are just the task count
                &times; 3 (every task starts at size M). Set sizes on tasks to make this differ from Tasks.
              </p>
            ) : null}

            {velocity!.truncated ? (
              <p className="text-muted-foreground text-xs">Showing the most recent 52 periods only.</p>
            ) : null}

            <ChartContainer
              config={velocityChartConfig}
              className="aspect-auto w-full"
              style={{ height: `${PERIOD_CHART_HEIGHT}px` }}
            >
              <ComposedChart
                data={chartData}
                margin={{ left: 4, right: 8 }}
                maxBarSize={BAR_MAX_SIZE}
                barCategoryGap={BAR_CATEGORY_GAP}
              >
                <CartesianGrid vertical={false} />
                <XAxis dataKey="row" tickLine={false} axisLine={false} fontSize={11} />
                <YAxis tickLine={false} axisLine={false} width={32} allowDecimals={false} />
                <ChartTooltip content={<ChartTooltipContent />} />
                <ChartLegend content={<ChartLegendContent />} itemSorter={velocityLegendItemSorter} />
                <Bar dataKey="completedByUser" stackId="completed" fill="var(--color-completedByUser)">
                  {chartData.map((entry, index) => (
                    <Cell key={`user-${index}`} fillOpacity={barOpacity(entry)} />
                  ))}
                </Bar>
                <Bar dataKey="completedByAgent" stackId="completed" fill="var(--color-completedByAgent)">
                  {chartData.map((entry, index) => (
                    <Cell key={`agent-${index}`} fillOpacity={barOpacity(entry)} />
                  ))}
                </Bar>
                <Bar dataKey="completedByUnknown" stackId="completed" fill="var(--color-completedByUnknown)">
                  {chartData.map((entry, index) => (
                    <Cell key={`unknown-${index}`} fillOpacity={barOpacity(entry)} />
                  ))}
                </Bar>
                <Line
                  type="monotone"
                  dataKey="movingAverage"
                  stroke="var(--color-movingAverage)"
                  dot={false}
                  strokeWidth={2}
                />
              </ComposedChart>
            </ChartContainer>
          </>
        )}
      </CardContent>
    </Card>
  );
}
