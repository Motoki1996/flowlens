"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { DeliveryMetrics, FlowMetrics } from "@/types";
import { fromDateParam, toDateParam } from "@/lib/dates";
import { DateField } from "@/components/DateField";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";

/** The six work stages, stacked in pipeline order — see app/globals.css's
 *  --chart-N assignment comment. Color slots are fixed and never cycled
 *  (slot 1 is the stage a task itself hits first), so the two backlog-level
 *  stages issue #173 added get the newer slots 6-7 rather than reusing 1-4,
 *  even though they render leftmost in the stack: backlogWaitingToStart and
 *  taskBreakdown happen before a task even exists. */
const stageChartConfig = {
  backlogWaitingToStart: { label: "Backlog waiting to start", color: "var(--chart-6)" },
  taskBreakdown: { label: "Task breakdown", color: "var(--chart-7)" },
  waitingToStart: { label: "Waiting to start", color: "var(--chart-1)" },
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
 *   stage lead time, from `task_progress_events` and `merge_requests`,
 *   drawn as a stacked horizontal (value-stream) bar so the slowest stage
 *   is visually obvious. Median and p90 are drawn as separate rows rather
 *   than folded together, so "always slow" (both rows tall) reads
 *   differently from "occasionally stuck" (p90 tall, median short). Blocked
 *   (on_hold) time is charted separately from the stage stack, never
 *   folded in, so it's never double-counted against the stage it
 *   interrupted. Two backlog-level stages (issue #173) — backlogWaitingToStart
 *   and taskBreakdown, from `backlog_progress_events` — extend the same
 *   stack one step earlier in the pipeline, before a task even exists.
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
  error = false,
}: {
  metrics: DeliveryMetrics | null;
  flowMetrics: FlowMetrics | null;
  from?: string;
  to?: string;
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

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
    ? [
        flowMetrics.backlogWaitingToStart,
        flowMetrics.taskBreakdown,
        flowMetrics.waitingToStart,
        flowMetrics.implementation,
        flowMetrics.reviewAndMerge,
        flowMetrics.completion,
      ]
    : [];
  const hasStageData = stageStats.some((s) => s.count > 0);
  const hasBlockedData = !!flowMetrics && flowMetrics.blocked.count > 0;
  const hasData = hasStatData || hasStageData || hasBlockedData;

  const stageChartData = flowMetrics
    ? [
        {
          row: "Median",
          backlogWaitingToStart: flowMetrics.backlogWaitingToStart.medianHours ?? 0,
          taskBreakdown: flowMetrics.taskBreakdown.medianHours ?? 0,
          waitingToStart: flowMetrics.waitingToStart.medianHours ?? 0,
          implementation: flowMetrics.implementation.medianHours ?? 0,
          reviewAndMerge: flowMetrics.reviewAndMerge.medianHours ?? 0,
          completion: flowMetrics.completion.medianHours ?? 0,
        },
        {
          row: "p90",
          backlogWaitingToStart: flowMetrics.backlogWaitingToStart.p90Hours ?? 0,
          taskBreakdown: flowMetrics.taskBreakdown.p90Hours ?? 0,
          waitingToStart: flowMetrics.waitingToStart.p90Hours ?? 0,
          implementation: flowMetrics.implementation.p90Hours ?? 0,
          reviewAndMerge: flowMetrics.reviewAndMerge.p90Hours ?? 0,
          completion: flowMetrics.completion.p90Hours ?? 0,
        },
      ]
    : [];

  const blockedChartData = flowMetrics
    ? [
        { row: "Median", blocked: flowMetrics.blocked.medianHours ?? 0 },
        { row: "p90", blocked: flowMetrics.blocked.p90Hours ?? 0 },
      ]
    : [];

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

            <div>
              <h3 className="text-foreground mb-3 text-sm font-medium">Stage lead time</h3>
              {hasStageData ? (
                <ChartContainer config={stageChartConfig} className="aspect-auto h-40 w-full">
                  <BarChart data={stageChartData} layout="vertical" margin={{ left: 8, right: 8 }}>
                    <CartesianGrid horizontal={false} />
                    <XAxis type="number" tickLine={false} axisLine={false} tickFormatter={formatHours} />
                    <YAxis dataKey="row" type="category" tickLine={false} axisLine={false} width={56} />
                    <ChartTooltip
                      content={<ChartTooltipContent formatter={(value) => formatHours(value as number)} />}
                    />
                    <ChartLegend content={<ChartLegendContent />} />
                    <Bar dataKey="backlogWaitingToStart" stackId="stage" fill="var(--color-backlogWaitingToStart)" />
                    <Bar dataKey="taskBreakdown" stackId="stage" fill="var(--color-taskBreakdown)" />
                    <Bar dataKey="waitingToStart" stackId="stage" fill="var(--color-waitingToStart)" />
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
                <ChartContainer config={blockedChartConfig} className="aspect-auto h-24 w-full">
                  <BarChart data={blockedChartData} layout="vertical" margin={{ left: 8, right: 8 }}>
                    <CartesianGrid horizontal={false} />
                    <XAxis type="number" tickLine={false} axisLine={false} tickFormatter={formatHours} />
                    <YAxis dataKey="row" type="category" tickLine={false} axisLine={false} width={56} />
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
