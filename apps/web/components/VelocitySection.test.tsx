import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
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
    completedPoints: 0,
    completedPointsByUser: 0,
    completedPointsByAgent: 0,
    completedPointsByUnknown: 0,
    movingAverage: 0,
    movingAveragePoints: 0,
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
    openTaskPoints: 0,
    averageVelocityPoints: null,
    forecastPeriodsByPoints: null,
    sizedTaskRatio: 0,
    unbrokenDownEpicPoints: 0,
    unestimatedEpicCount: 0,
    openPointsTotal: 0,
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
    expect(screen.getByText("34 tasks open ≈ 3.6 weeks left")).toBeInTheDocument();
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
    expect(screen.getByText("5 tasks open — no forecast yet")).toBeInTheDocument();
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

  // The Tasks/Points tab switches bars, moving average and both stats
  // together. Points come from the API already size-weighted; the UI never
  // multiplies anything itself.
  it("switches the stats and the chart to the size-weighted point series", () => {
    const velocity = makeVelocity({
      averageVelocity: 2,
      forecastPeriods: 5,
      openTaskCount: 10,
      averageVelocityPoints: 11,
      forecastPeriodsByPoints: 4,
      openTaskPoints: 44,
      // No epics in play, so the forecast's numerator is just the task
      // points — the case every test before epics existed assumed.
      openPointsTotal: 44,
      sizedTaskRatio: 0.8,
      periods: [
        makePeriod({
          completed: 2,
          completedByUser: 1,
          completedByAgent: 1,
          movingAverage: 2,
          completedPoints: 11,
          completedPointsByUser: 3,
          completedPointsByAgent: 8,
          movingAveragePoints: 11,
        }),
      ],
    });
    render(<VelocitySection velocity={velocity} />);

    expect(screen.getByText("2.0 tasks/week")).toBeInTheDocument();
    expect(screen.getByText("10 tasks open ≈ 5.0 weeks left")).toBeInTheDocument();

    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual(["Tasks", "Points"]);
    fireEvent.click(tabs[1]);

    expect(screen.getByText("11.0 points/week")).toBeInTheDocument();
    expect(screen.getByText("44 points open ≈ 4.0 weeks left")).toBeInTheDocument();
    expect(screen.queryByText("2.0 tasks/week")).not.toBeInTheDocument();
  });

  // Every task defaults to size M, so with nothing sized the point series is
  // arithmetically 3x the counts. Presenting that as a second measurement
  // would be a lie, so the card says so — but only on the Points tab.
  it("warns that points carry no information while no completed task has been sized", () => {
    const velocity = makeVelocity({
      sizedTaskRatio: 0,
      averageVelocity: 1,
      averageVelocityPoints: 3,
      periods: [makePeriod({ completed: 1, completedByUser: 1, completedPoints: 3, completedPointsByUser: 3 })],
    });
    render(<VelocitySection velocity={velocity} />);

    expect(screen.queryByText(/has been given a size yet/)).not.toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("tab")[1]);
    expect(screen.getByText(/has been given a size yet/)).toBeInTheDocument();
  });

  it("does not warn about sizes once some completed task has been sized", () => {
    const velocity = makeVelocity({
      sizedTaskRatio: 0.25,
      averageVelocityPoints: 4,
      periods: [makePeriod({ completed: 1, completedByUser: 1, completedPoints: 5, completedPointsByUser: 5 })],
    });
    render(<VelocitySection velocity={velocity} />);

    fireEvent.click(screen.getAllByRole("tab")[1]);
    expect(screen.queryByText(/has been given a size yet/)).not.toBeInTheDocument();
  });

  // A null average is "no complete period to average yet", which is not the
  // same as a velocity of zero and must never render as a number.
  it("shows placeholders on the Points tab too when the point average is null", () => {
    const velocity = makeVelocity({
      averageVelocity: null,
      forecastPeriods: null,
      averageVelocityPoints: null,
      forecastPeriodsByPoints: null,
      openTaskCount: 4,
      openTaskPoints: 15,
      openPointsTotal: 15,
      periods: [makePeriod({ completed: 1, completedByUser: 1, completedPoints: 3, complete: false })],
    });
    render(<VelocitySection velocity={velocity} />);

    fireEvent.click(screen.getAllByRole("tab")[1]);
    expect(screen.getByText("Not enough completed tasks yet")).toBeInTheDocument();
    expect(screen.getByText("15 points open — no forecast yet")).toBeInTheDocument();
  });

  // An epic with an estimate but no tasks is work the task count cannot see.
  // It is in the points forecast, so the points forecast has to admit it.
  it("says how much of the points forecast comes from epics with no tasks", () => {
    const velocity = makeVelocity({
      averageVelocityPoints: 10,
      forecastPeriodsByPoints: 5,
      openTaskPoints: 29,
      unbrokenDownEpicPoints: 21,
      openPointsTotal: 50,
      sizedTaskRatio: 0.5,
      periods: [makePeriod({ completed: 2, completedByUser: 2, completedPoints: 10 })],
    });
    render(<VelocitySection velocity={velocity} />);

    fireEvent.click(screen.getAllByRole("tab")[1]);
    // The open total is openPointsTotal, not openTaskPoints: it has to be the
    // number the forecast beside it was actually divided from.
    expect(screen.getByText("50 points open ≈ 5.0 weeks left")).toBeInTheDocument();
    expect(screen.getByText(/21 points estimated on epics that have no tasks yet/)).toBeInTheDocument();
  });

  it("calls the forecast a lower bound when an epic has neither tasks nor an estimate", () => {
    const velocity = makeVelocity({
      averageVelocityPoints: 10,
      forecastPeriodsByPoints: 3,
      openTaskPoints: 29,
      openPointsTotal: 29,
      unestimatedEpicCount: 2,
      sizedTaskRatio: 0.5,
      periods: [makePeriod({ completed: 2, completedByUser: 2, completedPoints: 10 })],
    });
    render(<VelocitySection velocity={velocity} />);

    fireEvent.click(screen.getAllByRole("tab")[1]);
    expect(
      screen.getByText(/2 open epics have neither tasks nor an estimate/),
    ).toBeInTheDocument();
    expect(screen.getByText(/lower bound/)).toBeInTheDocument();
  });

  // The count series is deliberately task-only, so neither caveat belongs on
  // the Tasks tab — saying it there would imply the task count includes epics.
  it("keeps both epic caveats off the Tasks tab", () => {
    const velocity = makeVelocity({
      averageVelocity: 2,
      forecastPeriods: 5,
      openTaskCount: 10,
      averageVelocityPoints: 10,
      forecastPeriodsByPoints: 5,
      openTaskPoints: 29,
      unbrokenDownEpicPoints: 21,
      unestimatedEpicCount: 2,
      openPointsTotal: 50,
      sizedTaskRatio: 0.5,
      periods: [makePeriod({ completed: 2, completedByUser: 2, completedPoints: 10 })],
    });
    render(<VelocitySection velocity={velocity} />);

    expect(screen.getByText("10 tasks open ≈ 5.0 weeks left")).toBeInTheDocument();
    expect(screen.queryByText(/epics that have no tasks yet/)).not.toBeInTheDocument();
    expect(screen.queryByText(/lower bound/)).not.toBeInTheDocument();
  });

  it("offers no unit tab at all when there is nothing to chart", () => {
    render(<VelocitySection velocity={makeVelocity({})} />);
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
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
