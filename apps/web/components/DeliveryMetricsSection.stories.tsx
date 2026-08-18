import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import type { DeliveryMetrics, FlowMetrics } from "@/types";
import { DeliveryMetricsSection } from "./DeliveryMetricsSection";

function makeMetrics(overrides: Partial<DeliveryMetrics>): DeliveryMetrics {
  return {
    from: null,
    to: null,
    interval: null,
    truncated: false,
    periods: [],
    openToFirstReview: { count: 0, medianHours: null, p90Hours: null },
    firstReviewToMerge: { count: 0, medianHours: null, p90Hours: null },
    additions: { count: 0, median: null, p90: null },
    deletions: { count: 0, median: null, p90: null },
    changedFiles: { count: 0, median: null, p90: null },
    pipelineSuccessRate: null,
    throughput: 0,
    ...overrides,
  };
}

function makeFlowMetrics(overrides: Partial<FlowMetrics>): FlowMetrics {
  return {
    from: null,
    to: null,
    interval: null,
    truncated: false,
    periods: [],
    backlogWaitingToStart: { count: 0, medianHours: null, p90Hours: null },
    taskBreakdown: { count: 0, medianHours: null, p90Hours: null },
    waitingToStart: { count: 0, medianHours: null, p90Hours: null },
    design: { count: 0, medianHours: null, p90Hours: null },
    implementation: { count: 0, medianHours: null, p90Hours: null },
    reviewAndMerge: { count: 0, medianHours: null, p90Hours: null },
    completion: { count: 0, medianHours: null, p90Hours: null },
    blocked: { count: 0, medianHours: null, p90Hours: null },
    ...overrides,
  };
}

/** stageStats builds one period's worth of task-level stage stats, for the
 *  MonthlyTrend story below — every non-design/blocked stage stays empty so
 *  the shrinking Design segment reads clearly across months. */
function stageStats(designMedian: number, designP90: number, blockedMedian: number, blockedP90: number) {
  const empty = { count: 0, medianHours: null, p90Hours: null };
  return {
    backlogWaitingToStart: empty,
    taskBreakdown: empty,
    waitingToStart: empty,
    design: { count: 6, medianHours: designMedian, p90Hours: designP90 },
    implementation: empty,
    reviewAndMerge: empty,
    completion: empty,
    blocked: { count: 2, medianHours: blockedMedian, p90Hours: blockedP90 },
  };
}

const meta = {
  title: "Components/DeliveryMetricsSection",
  component: DeliveryMetricsSection,
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: "/projects/p1" } },
  },
} satisfies Meta<typeof DeliveryMetricsSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** NoData: no merge requests or task progress synced yet, or none in the
 *  selected range. */
export const NoData: Story = {
  args: { metrics: makeMetrics({}), flowMetrics: makeFlowMetrics({}) },
};

/** Few: a single task has moved through every stage, so median and p90 are
 *  identical — the smallest non-empty distribution. */
export const Few: Story = {
  args: {
    metrics: makeMetrics({
      pipelineSuccessRate: 1,
      throughput: 1,
    }),
    flowMetrics: makeFlowMetrics({
      backlogWaitingToStart: { count: 1, medianHours: 3, p90Hours: 3 },
      taskBreakdown: { count: 1, medianHours: 1, p90Hours: 1 },
      waitingToStart: { count: 1, medianHours: 4, p90Hours: 4 },
      design: { count: 1, medianHours: 2, p90Hours: 2 },
      implementation: { count: 1, medianHours: 6, p90Hours: 6 },
      reviewAndMerge: { count: 1, medianHours: 2, p90Hours: 2 },
      completion: { count: 1, medianHours: 0.5, p90Hours: 0.5 },
    }),
  },
};

/** Normal: a healthy stream of tasks, with implementation the clear
 *  bottleneck (tall stack segment) and p90 well above the median on every
 *  stage — the shape a real team's lead time distribution takes. Blocked
 *  time is present too, shown in its own separate chart. */
export const Normal: Story = {
  args: {
    metrics: makeMetrics({
      pipelineSuccessRate: 0.92,
      throughput: 37,
    }),
    flowMetrics: makeFlowMetrics({
      backlogWaitingToStart: { count: 6, medianHours: 24, p90Hours: 96 },
      taskBreakdown: { count: 5, medianHours: 8, p90Hours: 40 },
      waitingToStart: { count: 30, medianHours: 12, p90Hours: 48 },
      design: { count: 22, medianHours: 16, p90Hours: 60 },
      implementation: { count: 28, medianHours: 40, p90Hours: 96 },
      reviewAndMerge: { count: 28, medianHours: 8, p90Hours: 30 },
      completion: { count: 27, medianHours: 1, p90Hours: 4 },
      blocked: { count: 9, medianHours: 6, p90Hours: 24 },
    }),
  },
};

/** MonthlyTrend: `?interval=month` turns the summary bars into one row per
 *  month (oldest on top), so the shrinking Design segment and climbing
 *  pipeline success rate read as improvement over time rather than a single
 *  snapshot (issue #189). Blocked time follows the same three months. */
export const MonthlyTrend: Story = {
  args: {
    interval: "month",
    metrics: makeMetrics({
      pipelineSuccessRate: 0.97,
      throughput: 14,
      periods: [
        {
          start: "2026-01-01T00:00:00Z",
          end: "2026-02-01T00:00:00Z",
          openToFirstReview: { count: 0, medianHours: null, p90Hours: null },
          firstReviewToMerge: { count: 0, medianHours: null, p90Hours: null },
          additions: { count: 0, median: null, p90: null },
          deletions: { count: 0, median: null, p90: null },
          changedFiles: { count: 0, median: null, p90: null },
          pipelineSuccessRate: 0.75,
          throughput: 4,
        },
        {
          start: "2026-02-01T00:00:00Z",
          end: "2026-03-01T00:00:00Z",
          openToFirstReview: { count: 0, medianHours: null, p90Hours: null },
          firstReviewToMerge: { count: 0, medianHours: null, p90Hours: null },
          additions: { count: 0, median: null, p90: null },
          deletions: { count: 0, median: null, p90: null },
          changedFiles: { count: 0, median: null, p90: null },
          pipelineSuccessRate: 0.88,
          throughput: 5,
        },
        {
          start: "2026-03-01T00:00:00Z",
          end: "2026-04-01T00:00:00Z",
          openToFirstReview: { count: 0, medianHours: null, p90Hours: null },
          firstReviewToMerge: { count: 0, medianHours: null, p90Hours: null },
          additions: { count: 0, median: null, p90: null },
          deletions: { count: 0, median: null, p90: null },
          changedFiles: { count: 0, median: null, p90: null },
          pipelineSuccessRate: 0.97,
          throughput: 5,
        },
      ],
    }),
    flowMetrics: makeFlowMetrics({
      design: { count: 18, medianHours: 20, p90Hours: 64 },
      blocked: { count: 6, medianHours: 8, p90Hours: 30 },
      periods: [
        { start: "2026-01-01T00:00:00Z", end: "2026-02-01T00:00:00Z", ...stageStats(56, 96, 18, 48) },
        { start: "2026-02-01T00:00:00Z", end: "2026-03-01T00:00:00Z", ...stageStats(28, 60, 8, 24) },
        { start: "2026-03-01T00:00:00Z", end: "2026-04-01T00:00:00Z", ...stageStats(12, 30, 2, 8) },
      ],
    }),
  },
};

/** Error: the metrics request failed; the rest of the Project view still
 *  renders around it. */
export const Error: Story = {
  args: { metrics: null, flowMetrics: null, error: true },
};
