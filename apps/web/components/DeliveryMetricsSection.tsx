"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { DeliveryMetrics } from "@/types";
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

/** median/p90 are two series of the same measure (lead time), not two
 *  statuses, so they take the first two categorical slots in fixed order —
 *  see app/globals.css's --chart-N assignment comment. */
const chartConfig = {
  median: { label: "Median", color: "var(--chart-1)" },
  p90: { label: "p90", color: "var(--chart-2)" },
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
 * aggregation (issue #113, ADR-0011 §3): review/merge lead time (median and
 * p90 — never a mean, since lead time is reliably skewed by a handful of
 * slow reviews), pipeline success rate and merge throughput, over an
 * optional date range held in the URL, the same
 * hand-off-through-the-URL/server-refetch pattern MergeRequestListSection
 * uses for its own filters. Merge-request size distribution
 * (additions/deletions/changed files) is part of the API response but not
 * charted here yet: internal/mrsync doesn't fetch GitLab's diff stats, so
 * every merge request's size is 0 today (see README's "Delivery metrics"
 * section) — charting all-zero data would be misleading, not informative.
 */
export function DeliveryMetricsSection({
  metrics,
  from,
  to,
  error = false,
}: {
  metrics: DeliveryMetrics | null;
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

  const hasData =
    !!metrics &&
    (metrics.openToFirstReview.count > 0 || metrics.firstReviewToMerge.count > 0 || metrics.throughput > 0);

  const chartData = metrics
    ? [
        {
          stage: "Open → first review",
          median: metrics.openToFirstReview.medianHours,
          p90: metrics.openToFirstReview.p90Hours,
        },
        {
          stage: "First review → merge",
          median: metrics.firstReviewToMerge.medianHours,
          p90: metrics.firstReviewToMerge.p90Hours,
        },
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
        ) : !metrics || !hasData ? (
          <p className="text-muted-foreground text-sm">
            No merge requests synced in this range yet. Metrics appear once merge-request sync (see the GitLab
            connection) has imported some.
          </p>
        ) : (
          <>
            <dl className="grid grid-cols-2 gap-x-8 gap-y-3 text-sm sm:grid-cols-4">
              <div>
                <dt className="text-muted-foreground">Throughput</dt>
                <dd className="text-foreground font-medium">{metrics.throughput} merged</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Pipeline success rate</dt>
                <dd className="text-foreground font-medium">{formatPercent(metrics.pipelineSuccessRate)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Open → first review (median)</dt>
                <dd className="text-foreground font-medium">
                  {formatHours(metrics.openToFirstReview.medianHours)}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">First review → merge (median)</dt>
                <dd className="text-foreground font-medium">
                  {formatHours(metrics.firstReviewToMerge.medianHours)}
                </dd>
              </div>
            </dl>

            <ChartContainer config={chartConfig} className="aspect-auto h-64 w-full">
              <BarChart data={chartData} margin={{ left: 8, right: 8 }}>
                <CartesianGrid vertical={false} />
                <XAxis dataKey="stage" tickLine={false} axisLine={false} />
                <YAxis
                  tickLine={false}
                  axisLine={false}
                  width={48}
                  tickFormatter={(value: number) => formatHours(value)}
                />
                <ChartTooltip content={<ChartTooltipContent formatter={(value) => formatHours(value as number)} />} />
                <ChartLegend content={<ChartLegendContent />} />
                <Bar dataKey="median" fill="var(--color-median)" radius={4} />
                <Bar dataKey="p90" fill="var(--color-p90)" radius={4} />
              </BarChart>
            </ChartContainer>
          </>
        )}
      </CardContent>
    </Card>
  );
}
