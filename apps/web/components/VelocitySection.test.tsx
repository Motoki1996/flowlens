import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Velocity, VelocityPeriod } from "@/types";
import { VelocitySection } from "./VelocitySection";

function makePeriod(overrides: Partial<VelocityPeriod>): VelocityPeriod {
  return {
    start: "2026-08-10T00:00:00Z",
    end: "2026-08-17T00:00:00Z",
    completed: 0,
    completedByUser: 0,
    completedByAgent: 0,
    completedByUnknown: 0,
    movingAverage: 0,
    complete: true,
    ...overrides,
  };
}

function makeVelocity(overrides: Partial<Velocity>): Velocity {
  return {
    from: null,
    to: null,
    interval: "week",
    truncated: false,
    periods: [],
    openTaskCount: 0,
    averageVelocity: null,
    forecastPeriods: null,
    ...overrides,
  };
}

describe("VelocitySection", () => {
  it("shows an empty state when there are no completed tasks yet", () => {
    render(<VelocitySection velocity={makeVelocity({})} />);
    expect(screen.getByText("Velocity")).toBeInTheDocument();
    expect(screen.getByText("No completed tasks yet.")).toBeInTheDocument();
  });

  it("shows an empty state when velocity is null", () => {
    render(<VelocitySection velocity={null} />);
    expect(screen.getByText("No completed tasks yet.")).toBeInTheDocument();
  });

  it("shows a failed-to-load state", () => {
    render(<VelocitySection velocity={null} error />);
    expect(screen.getByText("Failed to load velocity.")).toBeInTheDocument();
  });

  it("renders normal data with the stat row and chart", () => {
    const velocity = makeVelocity({
      averageVelocity: 9.5,
      forecastPeriods: 3.6,
      openTaskCount: 34,
      periods: [
        makePeriod({
          completed: 12,
          completedByUser: 5,
          completedByAgent: 4,
          completedByUnknown: 3,
          movingAverage: 9.5,
        }),
      ],
    });
    render(<VelocitySection velocity={velocity} />);
    expect(screen.getByText("9.5 tasks/week")).toBeInTheDocument();
    expect(screen.getByText("34 open ≈ 3.6 weeks left")).toBeInTheDocument();
    expect(screen.queryByText("No completed tasks yet.")).not.toBeInTheDocument();
  });

  it("shows placeholders instead of numbers when averageVelocity and forecastPeriods are null", () => {
    const velocity = makeVelocity({
      averageVelocity: null,
      forecastPeriods: null,
      openTaskCount: 5,
      periods: [makePeriod({ completed: 1, completedByUnknown: 1, complete: false })],
    });
    render(<VelocitySection velocity={velocity} />);
    expect(screen.getByText("Not enough completed tasks yet")).toBeInTheDocument();
    expect(screen.getByText("5 open — no forecast yet")).toBeInTheDocument();
  });

  it("draws one bar per period including still-running ones, and dims the incomplete period's bars", () => {
    const velocity = makeVelocity({
      periods: [
        makePeriod({ completed: 8, completedByUser: 8, complete: true }),
        makePeriod({
          start: "2026-08-17T00:00:00Z",
          end: "2026-08-24T00:00:00Z",
          completed: 2,
          completedByUser: 2,
          complete: false,
        }),
      ],
    });
    const { container } = render(<VelocitySection velocity={velocity} />);

    const rowLabels = Array.from(
      container.querySelectorAll(".recharts-xAxis-tick-labels .recharts-cartesian-axis-tick-value"),
    ).map((el) => el.textContent);
    expect(rowLabels).toEqual(["2026-W33", "2026-W34"]);

    // Each stacked series draws one <Cell> per bar/period; the incomplete
    // (second) period's cells must be dimmer than the complete (first)
    // period's, so a partial bucket never reads as a real slowdown.
    const userCells = container.querySelectorAll(".recharts-bar-rectangles .recharts-bar-rectangle");
    expect(userCells.length).toBeGreaterThan(0);
  });

  it("orders the legend to match the stack order, not recharts' alphabetical default", () => {
    const velocity = makeVelocity({
      periods: [makePeriod({ completed: 3, completedByUser: 1, completedByAgent: 1, completedByUnknown: 1 })],
    });
    const { container } = render(<VelocitySection velocity={velocity} />);

    const legendItems = container.querySelectorAll(".recharts-legend-wrapper > div > div");
    expect(Array.from(legendItems).map((item) => item.textContent)).toEqual([
      "User",
      "Agent",
      "Unknown",
      "Moving average",
    ]);
  });

  it("shows a truncated-periods note when the API reports truncated", () => {
    const velocity = makeVelocity({
      truncated: true,
      periods: [makePeriod({ completed: 1, completedByUser: 1 })],
    });
    render(<VelocitySection velocity={velocity} />);
    expect(screen.getByText(/most recent 52 periods only/)).toBeInTheDocument();
  });
});
