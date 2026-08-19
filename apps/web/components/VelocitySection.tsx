"use client";

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
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";

/** The three actor buckets a completed task falls into (issue #195), stacked
 *  bottom-to-top in this order — see globals.css's --chart-N comment. Reused
 *  from slots 1-3 (already used by DeliveryMetricsSection's own, separate
 *  stage chart; a chart's colors only need to be internally consistent, not
 *  globally unique — see GanttChart/TaskTimelineSection's own reuse of
 *  slot 1). */
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

/** formatVelocity renders averageVelocity the way the issue's own copy does:
 *  "9.5 tasks/week", pluralized by interval — or a placeholder once there is
 *  no complete period to average yet. */
function formatVelocity(averageVelocity: number | null, interval: MetricsInterval): string {
  if (averageVelocity == null) return "Not enough completed tasks yet";
  return `${averageVelocity.toFixed(1)} tasks/${interval}`;
}

/** formatForecast renders forecastPeriods + openTaskCount together: "34 open
 *  ≈ 3.6 weeks left" — null whenever averageVelocity is null or 0, in which
 *  case a forecast can't be made. */
function formatForecast(
  forecastPeriods: number | null,
  openTaskCount: number,
  interval: MetricsInterval,
): string {
  if (forecastPeriods == null) return `${openTaskCount} open — no forecast yet`;
  return `${openTaskCount} open ≈ ${forecastPeriods.toFixed(1)} ${interval}s left`;
}

const PERIOD_CHART_HEIGHT = 220;

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
 */
export function VelocitySection({
  velocity,
  error = false,
}: {
  velocity: Velocity | null;
  error?: boolean;
}) {
  const periods: VelocityPeriod[] = velocity?.periods ?? [];
  const hasData = periods.length > 0;
  const interval = velocity?.interval ?? "week";

  const chartData = periods.map((period) => ({
    row: periodLabel(period, interval),
    completedByUser: period.completedByUser,
    completedByAgent: period.completedByAgent,
    completedByUnknown: period.completedByUnknown,
    movingAverage: period.movingAverage,
    complete: period.complete,
  }));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Velocity</CardTitle>
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
                  {formatVelocity(velocity!.averageVelocity, interval)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Forecast</dt>
                <dd className="text-foreground font-medium">
                  {formatForecast(velocity!.forecastPeriods, velocity!.openTaskCount, interval)}
                </dd>
              </div>
            </dl>

            {velocity!.truncated ? (
              <p className="text-muted-foreground text-xs">Showing the most recent 52 periods only.</p>
            ) : null}

            <ChartContainer
              config={velocityChartConfig}
              className="aspect-auto w-full"
              style={{ height: `${PERIOD_CHART_HEIGHT}px` }}
            >
              <ComposedChart data={chartData} margin={{ left: 4, right: 8 }}>
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
