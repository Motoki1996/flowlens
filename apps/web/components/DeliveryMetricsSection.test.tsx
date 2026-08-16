import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { DeliveryMetrics, FlowMetrics } from "@/types";
import { DeliveryMetricsSection } from "./DeliveryMetricsSection";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
  usePathname: () => "/projects/p1",
  useSearchParams: () => new URLSearchParams(),
}));

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

function makeFlowMetrics(overrides: Partial<FlowMetrics>): FlowMetrics {
  return {
    from: null,
    to: null,
    waitingToStart: { count: 0, medianHours: null, p90Hours: null },
    implementation: { count: 0, medianHours: null, p90Hours: null },
    reviewAndMerge: { count: 0, medianHours: null, p90Hours: null },
    completion: { count: 0, medianHours: null, p90Hours: null },
    blocked: { count: 0, medianHours: null, p90Hours: null },
    ...overrides,
  };
}

describe("DeliveryMetricsSection", () => {
  beforeEach(() => {
    push.mockClear();
  });

  it("shows an empty state with no data", () => {
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={makeFlowMetrics({})} />);
    expect(screen.getByText("Delivery metrics")).toBeInTheDocument();
    expect(screen.getByText(/No merge requests or task progress synced in this range yet/)).toBeInTheDocument();
  });

  it("shows an empty state when both metrics are null", () => {
    render(<DeliveryMetricsSection metrics={null} flowMetrics={null} />);
    expect(screen.getByText(/No merge requests or task progress synced in this range yet/)).toBeInTheDocument();
  });

  it("shows a failed-to-load state", () => {
    render(<DeliveryMetricsSection metrics={null} flowMetrics={null} error />);
    expect(screen.getByText("Failed to load delivery metrics.")).toBeInTheDocument();
  });

  it("shows the stat row once delivery metrics have data, even with no stage history yet", () => {
    const metrics = makeMetrics({ pipelineSuccessRate: 1, throughput: 1 });
    render(<DeliveryMetricsSection metrics={metrics} flowMetrics={makeFlowMetrics({})} />);
    expect(screen.getByText("1 merged")).toBeInTheDocument();
    expect(screen.getByText("100%")).toBeInTheDocument();
    expect(screen.getByText(/No task progress history yet/)).toBeInTheDocument();
  });

  it("shows the stage lead-time chart for a single task's flow metrics", () => {
    const flowMetrics = makeFlowMetrics({
      waitingToStart: { count: 1, medianHours: 4, p90Hours: 4 },
      implementation: { count: 1, medianHours: 6, p90Hours: 6 },
      reviewAndMerge: { count: 1, medianHours: 2, p90Hours: 2 },
      completion: { count: 1, medianHours: 0.5, p90Hours: 0.5 },
    });
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={flowMetrics} />);
    expect(screen.getByText("Stage lead time")).toBeInTheDocument();
    expect(screen.queryByText(/No task progress history yet/)).not.toBeInTheDocument();
  });

  it("shows a separate blocked-time chart only when blocked time was recorded", () => {
    const { rerender } = render(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={makeFlowMetrics({
          waitingToStart: { count: 1, medianHours: 4, p90Hours: 4 },
        })}
      />,
    );
    expect(screen.queryByText("Blocked time")).not.toBeInTheDocument();

    rerender(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={makeFlowMetrics({
          waitingToStart: { count: 1, medianHours: 4, p90Hours: 4 },
          blocked: { count: 2, medianHours: 6, p90Hours: 24 },
        })}
      />,
    );
    expect(screen.getByText("Blocked time")).toBeInTheDocument();
  });

  it("updates the URL's from query param when a day is picked from the calendar", async () => {
    render(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={makeFlowMetrics({})}
        from="2026-01-05"
      />,
    );

    // The calendar opens on the picked date's month, so January 1st is one
    // click away. Day buttons are named by react-day-picker's own aria-label.
    fireEvent.click(screen.getByRole("button", { name: "From" }));
    fireEvent.click(await screen.findByRole("button", { name: /January 1st, 2026/ }));

    expect(push).toHaveBeenCalledWith("/projects/p1?from=2026-01-01");
  });

  it("shows the current range on the triggers and clears a date when its own day is re-picked", async () => {
    render(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={makeFlowMetrics({})}
        from="2026-01-05"
        to="2026-02-10"
      />,
    );

    expect(screen.getByRole("button", { name: "From" })).toHaveTextContent("Jan 5, 2026");
    expect(screen.getByRole("button", { name: "To" })).toHaveTextContent("Feb 10, 2026");

    fireEvent.click(screen.getByRole("button", { name: "To" }));
    fireEvent.click(await screen.findByRole("button", { name: /February 10th, 2026/ }));

    expect(push).toHaveBeenCalledWith("/projects/p1");
  });
});
