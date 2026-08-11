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

  it("updates the URL's from/to query params on date filter change", () => {
    render(<DeliveryMetricsSection metrics={makeMetrics({})} />);
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-01-01" } });
    expect(push).toHaveBeenCalledWith("/projects/p1?from=2026-01-01");
  });
});
