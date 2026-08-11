import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import type { DeliveryMetrics } from "@/types";
import { DeliveryMetricsSection } from "./DeliveryMetricsSection";

function makeMetrics(overrides: Partial<DeliveryMetrics>): DeliveryMetrics {
  return {
    from: null,
    to: null,
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

const meta = {
  title: "Components/DeliveryMetricsSection",
  component: DeliveryMetricsSection,
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: "/projects/p1" } },
  },
} satisfies Meta<typeof DeliveryMetricsSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** NoData: no merge requests synced yet, or none in the selected range. */
export const NoData: Story = {
  args: { metrics: makeMetrics({}) },
};

/** Few: a single merge request has been through review and merge, so
 *  median and p90 are identical — the smallest non-empty distribution. */
export const Few: Story = {
  args: {
    metrics: makeMetrics({
      openToFirstReview: { count: 1, medianHours: 2, p90Hours: 2 },
      firstReviewToMerge: { count: 1, medianHours: 1, p90Hours: 1 },
      pipelineSuccessRate: 1,
      throughput: 1,
    }),
  },
};

/** Normal: a healthy stream of merge requests, with p90 clearly above the
 *  median — the shape a real team's review time distribution takes. */
export const Normal: Story = {
  args: {
    metrics: makeMetrics({
      openToFirstReview: { count: 42, medianHours: 3.2, p90Hours: 30.5 },
      firstReviewToMerge: { count: 40, medianHours: 1.1, p90Hours: 10.4 },
      pipelineSuccessRate: 0.92,
      throughput: 37,
    }),
  },
};

/** Error: the metrics request failed; the rest of the Project view still
 *  renders around it. */
export const Error: Story = {
  args: { metrics: null, error: true },
};
