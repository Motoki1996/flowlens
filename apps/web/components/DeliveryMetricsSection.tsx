"use client";

import { useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
  type LegendPayload,
} from "recharts";
import type { DeliveryMetrics, DeliveryPeriod, FlowMetrics, FlowPeriod, MetricsInterval } from "@/types";
import { fromDateParam, periodLabel, toDateParam } from "@/lib/dates";
import { DateField } from "@/components/DateField";
import { MetricTabs } from "@/components/MetricTabs";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";

/** The four work stages this chart visualizes, stacked in pipeline order —
 *  see app/globals.css's --chart-N assignment comment. Deliberately starts
 *  at Design/Implementation, not at task creation: backlogWaitingToStart,
 *  taskBreakdown and waitingToStart (still computed by the API, issues #171
 *  and #173) are not charted here, only from-Implementation-onward lead
 *  time is. Design and Implementation are measured from task.designStartedAt
 *  and task.implementationStartedAt — explicit phase markers a caller sets
 *  via POST .../design-started and .../implementation-started for
 *  spec-driven development — rather than from a progress transition, so a
 *  task that never calls either endpoint simply has no Design/Implementation
 *  sample. Slot 1, freed by dropping waitingToStart from this chart, is
 *  reused for Design, now the leftmost/first-hit stage here. */
const stageChartConfig = {
  design: { label: "Design", color: "var(--chart-1)" },
  implementation: { label: "Implementation", color: "var(--chart-2)" },
  reviewAndMerge: { label: "Review & merge", color: "var(--chart-3)" },
  completion: { label: "Completion", color: "var(--chart-4)" },
} satisfies ChartConfig;

/** Blocked (on_hold) time is charted separately from the stage stack above
 *  so it is never double-counted against the stage it interrupted — see
 *  issue #172. It's a single series, so slot 5's protanopia caveat (see
 *  globals.css) doesn't apply. */
const blockedChartConfig = {
  blocked: { label: "Blocked (on hold)", color: "var(--chart-5)" },
} satisfies ChartConfig;

/** Throughput/success-rate trend charts (issue #189) reuse the two chart
 *  slots the stage chart's Design-onward narrowing freed up (6-7); each is
 *  a single series, so the protanopia pairing caveat in globals.css doesn't
 *  apply to either. */
const throughputChartConfig = {
  throughput: { label: "Throughput", color: "var(--chart-6)" },
} satisfies ChartConfig;

const successRateChartConfig = {
  successRate: { label: "Pipeline success rate", color: "var(--chart-7)" },
} satisfies ChartConfig;

/** recharts 3's <Legend> defaults to sorting entries alphabetically by label
 *  (`itemSorter: "value"`), which scrambles this value-stream chart's
 *  left-to-right stage order (e.g. "Completion" sorts before "Design"). Sort
 *  by each entry's position in stageChartConfig instead, so the legend always
 *  reads in the same order the bars are stacked. */
const stageKeys = Object.keys(stageChartConfig);
function stageLegendItemSorter(item: LegendPayload) {
  return stageKeys.indexOf(String(item.dataKey));
}

/** formatHours renders a duration the way a human reads lead time: minutes
 *  under an hour, hours under two days, days beyond that. Every place a
 *  duration is shown (stat row, chart axis, tooltip) goes through this. */
function formatHours(hours: number | null): string {
  if (hours == null) return "—";
  if (hours < 1) return `${Math.round(hours * 60)}m`;
  if (hours < 48) return `${hours.toFixed(1)}h`;
  return `${(hours / 24).toFixed(1)}d`;
}

function formatPercent(ratio: number | null): string {
  if (ratio == null) return "—";
  return `${Math.round(ratio * 100)}%`;
}

type StatTab = "median" | "p90";

/** Stage lead time and Blocked time switch between Median and p90 together,
 *  off one piece of state on purpose (issue #189): the two charts share one
 *  "way of reading" the distribution, so letting them switch independently
 *  would invite misreading one against the other's stat. */
const STAT_TABS: ReadonlyArray<{ key: StatTab; label: string }> = [
  { key: "median", label: "Median" },
  { key: "p90", label: "p90" },
];

function statValue(stats: { medianHours: number | null; p90Hours: number | null }, statTab: StatTab): number {
  return (statTab === "median" ? stats.medianHours : stats.p90Hours) ?? 0;
}

const PERIOD_ROW_HEIGHT = 28;

function periodChartHeightPx(periodCount: number): number {
  return Math.max(96, periodCount * PERIOD_ROW_HEIGHT + 40);
}

/**
 * DeliveryMetricsSection is the Project single view's delivery-flow
 * aggregation, over an optional date range held in the URL — the same
 * hand-off-through-the-URL/server-refetch pattern MergeRequestListSection
 * uses for its own filters. It combines two independent, read-only
 * aggregations that share the same [from, to] filter:
 *
 * - Delivery metrics (issue #113, ADR-0011 §3): pipeline success rate and
 *   merge throughput, from `merge_requests`.
 * - Flow metrics (issue #171, replacing this section's former open→first
 *   review/first review→merge grouped bar chart per issue #172): per-task
 *   stage lead time from Design onward, drawn as a stacked horizontal
 *   (value-stream) bar so the slowest stage is visually obvious. Design and
 *   Implementation are measured from task.designStartedAt/
 *   implementationStartedAt (explicit spec-driven-development phase
 *   markers a caller sets via POST .../design-started and
 *   .../implementation-started), Review & merge and Completion from
 *   `merge_requests`/`task_progress_events` as before. Blocked (on_hold)
 *   time is charted separately from the stage stack, never folded in, so
 *   it's never double-counted against the stage it interrupted. The API
 *   also reports three earlier stages — backlogWaitingToStart, taskBreakdown
 *   and waitingToStart (issues #171, #173) — but this chart deliberately
 *   starts at Design: only from-Implementation-onward lead time is
 *   visualized here.
 *
 * Median and p90 (issue #189) switch via one shared tab above both charts,
 * rather than drawing two rows at once, so a single period/range's bars stay
 * legible; the `?interval=week|month|year` selector (issue #188's period
 * bucketing) turns each chart from one summary row into one row per period,
 * oldest on top, so "is this improving?" reads top-to-bottom. `interval`
 * lives in the URL alongside `from`/`to`; the tab choice does not, since it's
 * a way of reading already-fetched data, not a filter on what was fetched.
 * `interval` also adds small throughput/pipeline-success-rate trend charts
 * below the stat row.
 *
 * Merge-request size distribution (additions/deletions/changed files) is
 * part of the delivery-metrics API response but not charted here: every
 * merge request's size is 0 today, since internal/mrsync doesn't fetch
 * GitLab's diff stats yet (see README's "Delivery metrics" section) —
 * charting all-zero data would be misleading, not informative.
 */
export function DeliveryMetricsSection({
  metrics,
  flowMetrics,
  from,
  to,
  interval,
  error = false,
}: {
  metrics: DeliveryMetrics | null;
  flowMetrics: FlowMetrics | null;
  from?: string;
  to?: string;
  interval?: MetricsInterval;
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [statTab, setStatTab] = useState<StatTab>("median");

  function updateQuery(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(next)) {
      params.delete(key);
      if (value) params.set(key, value);
    }
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  const hasStatData = !!metrics && (metrics.throughput > 0 || metrics.pipelineSuccessRate != null);
  const stageStats = flowMetrics
    ? [flowMetrics.design, flowMetrics.implementation, flowMetrics.reviewAndMerge, flowMetrics.completion]
    : [];
  const hasStageData = stageStats.some((s) => s.count > 0);
  const hasBlockedData = !!flowMetrics && flowMetrics.blocked.count > 0;
  const hasData = hasStatData || hasStageData || hasBlockedData;
  const truncated = !!metrics?.truncated || !!flowMetrics?.truncated;

  const stagePeriods: FlowPeriod[] = interval ? (flowMetrics?.periods ?? []) : [];
  const stageChartData = interval
    ? stagePeriods.map((period) => ({
        row: periodLabel(period, interval),
        design: statValue(period.design, statTab),
        implementation: statValue(period.implementation, statTab),
        reviewAndMerge: statValue(period.reviewAndMerge, statTab),
        completion: statValue(period.completion, statTab),
      }))
    : flowMetrics
      ? [
          {
            row: statTab === "median" ? "Median" : "p90",
            design: statValue(flowMetrics.design, statTab),
            implementation: statValue(flowMetrics.implementation, statTab),
            reviewAndMerge: statValue(flowMetrics.reviewAndMerge, statTab),
            completion: statValue(flowMetrics.completion, statTab),
          },
        ]
      : [];

  const blockedChartData = interval
    ? stagePeriods.map((period) => ({ row: periodLabel(period, interval), blocked: statValue(period.blocked, statTab) }))
    : flowMetrics
      ? [{ row: statTab === "median" ? "Median" : "p90", blocked: statValue(flowMetrics.blocked, statTab) }]
      : [];

  const trendPeriods: DeliveryPeriod[] = interval ? (metrics?.periods ?? []) : [];
  const throughputTrendData = trendPeriods.map((period) => ({
    period: periodLabel(period, interval as MetricsInterval),
    throughput: period.throughput,
  }));
  const successRateTrendData = trendPeriods.map((period) => ({
    period: periodLabel(period, interval as MetricsInterval),
    successRate: period.pipelineSuccessRate == null ? null : Math.round(period.pipelineSuccessRate * 100),
  }));

  const stageChartHeight = interval ? periodChartHeightPx(stagePeriods.length) : 96;
  const blockedChartHeight = interval ? periodChartHeightPx(stagePeriods.length) : 64;

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Delivery metrics</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <DateField
              id="delivery-metrics-from"
              label="From"
              placeholder="From"
              hideLabel
              className="h-8 w-40"
              value={fromDateParam(from)}
              onChange={(date) => updateQuery({ from: date ? toDateParam(date) : undefined })}
            />
            <DateField
              id="delivery-metrics-to"
              label="To"
              placeholder="To"
              hideLabel
              className="h-8 w-40"
              value={fromDateParam(to)}
              onChange={(date) => updateQuery({ to: date ? toDateParam(date) : undefined })}
            />
            <Select
              value={interval ?? "all"}
              onValueChange={(value) => updateQuery({ interval: value === "all" ? undefined : value })}
            >
              <SelectTrigger id="delivery-metrics-interval" aria-label="Interval" className="h-8 w-24">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All</SelectItem>
                <SelectItem value="week">Week</SelectItem>
                <SelectItem value="month">Month</SelectItem>
                <SelectItem value="year">Year</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-6">
        {error ? (
          <p className="text-destructive text-sm">Failed to load delivery metrics.</p>
        ) : !hasData ? (
          <p className="text-muted-foreground text-sm">
            No merge requests or task progress synced in this range yet. Metrics appear once merge-request sync
            and task progress events (see the GitLab connection) have some history.
          </p>
        ) : (
          <>
            <dl className="grid grid-cols-2 gap-x-8 gap-y-3 text-sm">
              <div>
                <dt className="text-muted-foreground">Throughput</dt>
                <dd className="text-foreground font-medium">{metrics?.throughput ?? 0} merged</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Pipeline success rate</dt>
                <dd className="text-foreground font-medium">
                  {formatPercent(metrics?.pipelineSuccessRate ?? null)}
                </dd>
              </div>
            </dl>

            {interval && trendPeriods.length > 0 ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <div>
                  <h4 className="text-muted-foreground mb-2 text-xs font-medium">Throughput trend</h4>
                  <ChartContainer config={throughputChartConfig} className="aspect-auto h-28 w-full">
                    <BarChart data={throughputTrendData} margin={{ left: 4, right: 8 }}>
                      <CartesianGrid vertical={false} />
                      <XAxis dataKey="period" tickLine={false} axisLine={false} fontSize={11} />
                      <YAxis tickLine={false} axisLine={false} width={28} allowDecimals={false} />
                      <ChartTooltip content={<ChartTooltipContent />} />
                      <Bar dataKey="throughput" fill="var(--color-throughput)" />
                    </BarChart>
                  </ChartContainer>
                </div>
                <div>
                  <h4 className="text-muted-foreground mb-2 text-xs font-medium">Pipeline success rate trend</h4>
                  <ChartContainer config={successRateChartConfig} className="aspect-auto h-28 w-full">
                    <LineChart data={successRateTrendData} margin={{ left: 4, right: 8 }}>
                      <CartesianGrid vertical={false} />
                      <XAxis dataKey="period" tickLine={false} axisLine={false} fontSize={11} />
                      <YAxis
                        domain={[0, 100]}
                        tickFormatter={(value) => `${value}%`}
                        tickLine={false}
                        axisLine={false}
                        width={36}
                      />
                      <ChartTooltip
                        content={<ChartTooltipContent formatter={(value) => `${value}%`} />}
                      />
                      <Line
                        type="monotone"
                        dataKey="successRate"
                        stroke="var(--color-successRate)"
                        connectNulls={false}
                        dot
                      />
                    </LineChart>
                  </ChartContainer>
                </div>
              </div>
            ) : null}

            {truncated ? (
              <p className="text-muted-foreground text-xs">Showing the most recent 52 periods only.</p>
            ) : null}

            <div>
              <div className="mb-3 flex items-center justify-between gap-3">
                <h3 className="text-foreground text-sm font-medium">Stage lead time</h3>
                <MetricTabs label="Statistic" tabs={STAT_TABS} value={statTab} onChange={setStatTab} />
              </div>
              {hasStageData ? (
                <ChartContainer
                  config={stageChartConfig}
                  className="aspect-auto w-full"
                  style={{ height: `${stageChartHeight}px` }}
                >
                  <BarChart data={stageChartData} layout="vertical" margin={{ left: 8, right: 8 }}>
                    <CartesianGrid horizontal={false} />
                    <XAxis type="number" tickLine={false} axisLine={false} tickFormatter={formatHours} />
                    <YAxis dataKey="row" type="category" tickLine={false} axisLine={false} width={64} />
                    <ChartTooltip
                      content={<ChartTooltipContent formatter={(value) => formatHours(value as number)} />}
                    />
                    <ChartLegend content={<ChartLegendContent />} itemSorter={stageLegendItemSorter} />
                    <Bar dataKey="design" stackId="stage" fill="var(--color-design)" />
                    <Bar dataKey="implementation" stackId="stage" fill="var(--color-implementation)" />
                    <Bar dataKey="reviewAndMerge" stackId="stage" fill="var(--color-reviewAndMerge)" />
                    <Bar dataKey="completion" stackId="stage" fill="var(--color-completion)" />
                  </BarChart>
                </ChartContainer>
              ) : (
                <p className="text-muted-foreground text-sm">
                  No task progress history yet. Stage lead time appears once tasks move through in_progress/done
                  (see the progress convention for agents).
                </p>
              )}
            </div>

            {hasBlockedData ? (
              <div>
                <h3 className="text-foreground mb-3 text-sm font-medium">Blocked time</h3>
                <ChartContainer
                  config={blockedChartConfig}
                  className="aspect-auto w-full"
                  style={{ height: `${blockedChartHeight}px` }}
                >
                  <BarChart data={blockedChartData} layout="vertical" margin={{ left: 8, right: 8 }}>
                    <CartesianGrid horizontal={false} />
                    <XAxis type="number" tickLine={false} axisLine={false} tickFormatter={formatHours} />
                    <YAxis dataKey="row" type="category" tickLine={false} axisLine={false} width={64} />
                    <ChartTooltip
                      content={<ChartTooltipContent formatter={(value) => formatHours(value as number)} />}
                    />
                    <Bar dataKey="blocked" fill="var(--color-blocked)" />
                  </BarChart>
                </ChartContainer>
              </div>
            ) : null}
          </>
        )}
      </CardContent>
    </Card>
  );
}
