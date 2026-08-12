import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { DeliveryMetrics } from "@/types";
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

describe("DeliveryMetricsSection", () => {
  beforeEach(() => {
    push.mockClear();
  });

  it("shows an empty state with no data", () => {
    render(<DeliveryMetricsSection metrics={makeMetrics({})} />);
    expect(screen.getByText("Delivery metrics")).toBeInTheDocument();
    expect(screen.getByText(/No merge requests synced in this range yet/)).toBeInTheDocument();
  });

  it("shows an empty state when metrics is null", () => {
    render(<DeliveryMetricsSection metrics={null} />);
    expect(screen.getByText(/No merge requests synced in this range yet/)).toBeInTheDocument();
  });

  it("shows a failed-to-load state", () => {
    render(<DeliveryMetricsSection metrics={null} error />);
    expect(screen.getByText("Failed to load delivery metrics.")).toBeInTheDocument();
  });

  it("shows the stat row for a single merge request's metrics", () => {
    const metrics = makeMetrics({
      openToFirstReview: { count: 1, medianHours: 2, p90Hours: 2 },
      firstReviewToMerge: { count: 1, medianHours: 1, p90Hours: 1 },
      pipelineSuccessRate: 1,
      throughput: 1,
    });
    render(<DeliveryMetricsSection metrics={metrics} />);
    expect(screen.getByText("1 merged")).toBeInTheDocument();
    expect(screen.getByText("100%")).toBeInTheDocument();
    expect(screen.getAllByText("2.0h").length).toBeGreaterThan(0);
    expect(screen.getAllByText("1.0h").length).toBeGreaterThan(0);
  });

  it("shows normal (many merge requests) metrics with a wide median/p90 spread", () => {
    const metrics = makeMetrics({
      openToFirstReview: { count: 5, medianHours: 1, p90Hours: 100 },
      firstReviewToMerge: { count: 4, medianHours: 3, p90Hours: 30 },
      pipelineSuccessRate: 0.75,
      throughput: 12,
    });
    render(<DeliveryMetricsSection metrics={metrics} />);
    expect(screen.getByText("12 merged")).toBeInTheDocument();
    expect(screen.getByText("75%")).toBeInTheDocument();
  });

  it("updates the URL's from query param when a day is picked from the calendar", async () => {
    render(<DeliveryMetricsSection metrics={makeMetrics({})} from="2026-01-05" />);

    // The calendar opens on the picked date's month, so January 1st is one
    // click away. Day buttons are named by react-day-picker's own aria-label.
    fireEvent.click(screen.getByRole("button", { name: "From" }));
    fireEvent.click(await screen.findByRole("button", { name: /January 1st, 2026/ }));

    expect(push).toHaveBeenCalledWith("/projects/p1?from=2026-01-01");
  });

  it("shows the current range on the triggers and clears a date when its own day is re-picked", async () => {
    render(<DeliveryMetricsSection metrics={makeMetrics({})} from="2026-01-05" to="2026-02-10" />);

    expect(screen.getByRole("button", { name: "From" })).toHaveTextContent("Jan 5, 2026");
    expect(screen.getByRole("button", { name: "To" })).toHaveTextContent("Feb 10, 2026");

    fireEvent.click(screen.getByRole("button", { name: "To" }));
    fireEvent.click(await screen.findByRole("button", { name: /February 10th, 2026/ }));

    expect(push).toHaveBeenCalledWith("/projects/p1");
  });
});
