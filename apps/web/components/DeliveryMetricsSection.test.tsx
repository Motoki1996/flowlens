import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { DeliveryMetrics, FlowMetrics } from "@/types";
import { DeliveryMetricsSection } from "./DeliveryMetricsSection";

const push = vi.fn();
let mockSearchParams = new URLSearchParams();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
  usePathname: () => "/projects/p1",
  useSearchParams: () => mockSearchParams,
}));

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

describe("DeliveryMetricsSection", () => {
  beforeEach(() => {
    push.mockClear();
    mockSearchParams = new URLSearchParams();
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
      design: { count: 1, medianHours: 3, p90Hours: 3 },
      implementation: { count: 1, medianHours: 6, p90Hours: 6 },
      reviewAndMerge: { count: 1, medianHours: 2, p90Hours: 2 },
      completion: { count: 1, medianHours: 0.5, p90Hours: 0.5 },
    });
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={flowMetrics} />);
    expect(screen.getByText("Stage lead time")).toBeInTheDocument();
    expect(screen.queryByText(/No task progress history yet/)).not.toBeInTheDocument();
  });

  // Only Design onward is visualized here (spec-driven development): the
  // three earlier stages the API still reports — backlogWaitingToStart,
  // taskBreakdown, waitingToStart — never appear in this legend.
  it("does not include the pre-Design stages in the stage lead-time legend", () => {
    const flowMetrics = makeFlowMetrics({
      backlogWaitingToStart: { count: 3, medianHours: 24, p90Hours: 96 },
      taskBreakdown: { count: 2, medianHours: 8, p90Hours: 40 },
      waitingToStart: { count: 5, medianHours: 4, p90Hours: 4 },
      design: { count: 1, medianHours: 3, p90Hours: 3 },
    });
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={flowMetrics} />);
    expect(screen.getByText("Stage lead time")).toBeInTheDocument();
    expect(screen.getByText("Design")).toBeInTheDocument();
    expect(screen.queryByText("Backlog waiting to start")).not.toBeInTheDocument();
    expect(screen.queryByText("Task breakdown")).not.toBeInTheDocument();
    expect(screen.queryByText("Waiting to start")).not.toBeInTheDocument();
  });

  it("orders the stage lead-time legend to match the stack order, not recharts' default", () => {
    const flowMetrics = makeFlowMetrics({
      design: { count: 1, medianHours: 3, p90Hours: 3 },
      implementation: { count: 1, medianHours: 6, p90Hours: 6 },
      reviewAndMerge: { count: 1, medianHours: 2, p90Hours: 2 },
      completion: { count: 1, medianHours: 0.5, p90Hours: 0.5 },
    });
    const { container } = render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={flowMetrics} />);

    const legendItems = container.querySelectorAll(".recharts-legend-wrapper > div > div");
    expect(Array.from(legendItems).map((item) => item.textContent)).toEqual([
      "Design",
      "Implementation",
      "Review & merge",
      "Completion",
    ]);
    // Each swatch's color must still track its own stage, not just its new position.
    expect(Array.from(legendItems).map((item) => item.querySelector("div")?.style.backgroundColor)).toEqual([
      "var(--color-design)",
      "var(--color-implementation)",
      "var(--color-reviewAndMerge)",
      "var(--color-completion)",
    ]);
  });

  it("shows a separate blocked-time chart only when blocked time was recorded", () => {
    const { rerender } = render(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={makeFlowMetrics({
          design: { count: 1, medianHours: 3, p90Hours: 3 },
        })}
      />,
    );
    expect(screen.queryByText("Blocked time")).not.toBeInTheDocument();

    rerender(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={makeFlowMetrics({
          design: { count: 1, medianHours: 3, p90Hours: 3 },
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

  it("pushes ?interval= when the interval selector changes", async () => {
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={makeFlowMetrics({})} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Interval" }));
    fireEvent.click(await screen.findByRole("option", { name: "Month" }));
    expect(push).toHaveBeenCalledWith("/projects/p1?interval=month");
  });

  it("clears ?interval= when All is picked", async () => {
    mockSearchParams = new URLSearchParams("interval=month");
    render(
      <DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={makeFlowMetrics({})} interval="month" />,
    );
    fireEvent.click(screen.getByRole("combobox", { name: "Interval" }));
    fireEvent.click(await screen.findByRole("option", { name: "All" }));
    expect(push).toHaveBeenCalledWith("/projects/p1");
  });

  it("draws one stage lead-time row per period, oldest first, including empty periods", () => {
    const flowMetrics = makeFlowMetrics({
      design: { count: 2, medianHours: 4, p90Hours: 8 },
      periods: [
        {
          start: "2026-01-01T00:00:00Z",
          end: "2026-02-01T00:00:00Z",
          backlogWaitingToStart: { count: 0, medianHours: null, p90Hours: null },
          taskBreakdown: { count: 0, medianHours: null, p90Hours: null },
          waitingToStart: { count: 0, medianHours: null, p90Hours: null },
          design: { count: 1, medianHours: 8, p90Hours: 8 },
          implementation: { count: 0, medianHours: null, p90Hours: null },
          reviewAndMerge: { count: 0, medianHours: null, p90Hours: null },
          completion: { count: 0, medianHours: null, p90Hours: null },
          blocked: { count: 0, medianHours: null, p90Hours: null },
        },
        {
          // A gap-filled empty period (count: 0 throughout) must still render
          // as its own row, so a gap reads as a gap rather than disappearing.
          start: "2026-02-01T00:00:00Z",
          end: "2026-03-01T00:00:00Z",
          backlogWaitingToStart: { count: 0, medianHours: null, p90Hours: null },
          taskBreakdown: { count: 0, medianHours: null, p90Hours: null },
          waitingToStart: { count: 0, medianHours: null, p90Hours: null },
          design: { count: 0, medianHours: null, p90Hours: null },
          implementation: { count: 0, medianHours: null, p90Hours: null },
          reviewAndMerge: { count: 0, medianHours: null, p90Hours: null },
          completion: { count: 0, medianHours: null, p90Hours: null },
          blocked: { count: 0, medianHours: null, p90Hours: null },
        },
      ],
    });
    render(
      <DeliveryMetricsSection
        metrics={makeMetrics({})}
        flowMetrics={flowMetrics}
        interval="month"
      />,
    );

    const rowLabels = Array.from(
      document.querySelectorAll(".recharts-yAxis-tick-labels .recharts-cartesian-axis-tick-value"),
    ).map((el) => el.textContent);
    expect(rowLabels).toEqual(["2026-01", "2026-02"]);
  });

  it("shows a truncated-periods note when the API reports truncated", () => {
    render(
      <DeliveryMetricsSection
        metrics={makeMetrics({ truncated: true })}
        flowMetrics={makeFlowMetrics({ design: { count: 1, medianHours: 1, p90Hours: 1 } })}
        interval="week"
      />,
    );
    expect(screen.getByText(/most recent 52 periods only/)).toBeInTheDocument();
  });

  it("switches both the stage and blocked charts' values together via the Median/p90 tabs", () => {
    const flowMetrics = makeFlowMetrics({
      design: { count: 2, medianHours: 4, p90Hours: 40 },
      blocked: { count: 2, medianHours: 2, p90Hours: 20 },
    });
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={flowMetrics} />);

    const stageSection = screen.getByRole("heading", { name: "Stage lead time" }).closest("div")!.parentElement!;
    const blockedSection = screen.getByRole("heading", { name: "Blocked time" }).closest("div")!;
    // Without ?interval=, each chart draws a single row keyed by the active
    // stat's own name — a reliable proxy (unlike a numeric axis tick, never
    // subject to recharts' own "nice round number" tick rounding) for which
    // stat a chart is currently drawing.
    function rowLabel(section: HTMLElement): string | null {
      return section.querySelector(".recharts-yAxis-tick-labels .recharts-cartesian-axis-tick-value")?.textContent ?? null;
    }

    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual(["Median", "p90"]);
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");
    expect(tabs[1]).toHaveAttribute("aria-selected", "false");
    expect(rowLabel(stageSection)).toBe("Median");
    expect(rowLabel(blockedSection)).toBe("Median");

    fireEvent.click(tabs[1]);
    expect(tabs[1]).toHaveAttribute("aria-selected", "true");
    expect(tabs[0]).toHaveAttribute("aria-selected", "false");
    expect(rowLabel(stageSection)).toBe("p90");
    expect(rowLabel(blockedSection)).toBe("p90");
  });

  it("moves the Median/p90 tab selection with the arrow keys", () => {
    const flowMetrics = makeFlowMetrics({ design: { count: 1, medianHours: 1, p90Hours: 2 } });
    render(<DeliveryMetricsSection metrics={makeMetrics({})} flowMetrics={flowMetrics} />);

    const [medianTab, p90Tab] = screen.getAllByRole("tab");
    medianTab.focus();
    expect(medianTab).toHaveFocus();

    fireEvent.keyDown(medianTab, { key: "ArrowRight" });
    expect(p90Tab).toHaveAttribute("aria-selected", "true");
    expect(p90Tab).toHaveFocus();
    expect(medianTab).toHaveAttribute("tabIndex", "-1");

    fireEvent.keyDown(p90Tab, { key: "ArrowLeft" });
    expect(medianTab).toHaveAttribute("aria-selected", "true");
    expect(medianTab).toHaveFocus();
  });
});
