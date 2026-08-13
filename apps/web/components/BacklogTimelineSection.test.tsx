import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { Backlog } from "@/types";
import { BacklogTimelineSection } from "./BacklogTimelineSection";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

// The date math and the progress ratio are covered in lib/timeline.test.ts;
// these tests cover what the DOM must offer regardless of how the bars are
// drawn — navigation to each backlog, the completion stated as text, the
// legend that keeps status readable without colour, and the backlogs the
// chart cannot show.
const NOW = new Date("2026-08-05T00:00:00Z");

function makeBacklog(overrides: Partial<Backlog>): Backlog {
  return {
    id: "b1",
    projectId: "p1",
    name: "Backlog",
    description: "",
    position: 0,
    startDate: null,
    dueOn: null,
    priority: "medium",
    progress: "not_started",
    taskCount: 0,
    closedTaskCount: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("BacklogTimelineSection", () => {
  it("shows a guidance message when no backlog has a schedule", () => {
    render(<BacklogTimelineSection projectId="p1" backlogs={[makeBacklog({})]} now={NOW} />);
    expect(
      screen.getByText(
        "No scheduled backlogs yet. Set a start date or due date on a backlog to see it on the timeline.",
      ),
    ).toBeInTheDocument();
  });

  it("links every scheduled backlog to its single view", () => {
    const backlogs = [
      makeBacklog({ id: "b1", name: "Sprint 1", startDate: "2026-08-01", dueOn: "2026-08-07" }),
      makeBacklog({ id: "b2", name: "Sprint 2", startDate: "2026-08-08", dueOn: "2026-08-14" }),
    ];
    render(<BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />);
    expect(screen.getByRole("link", { name: "Sprint 1" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b1",
    );
    expect(screen.getByRole("link", { name: "Sprint 2" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b2",
    );
  });

  // The fill is a second reading of the ratio, never the only one.
  it("states each backlog's completion as text beside its bar", () => {
    const backlogs = [
      makeBacklog({
        id: "b1",
        name: "Sprint 1",
        startDate: "2026-08-01",
        dueOn: "2026-08-07",
        taskCount: 4,
        closedTaskCount: 2,
      }),
    ];
    render(<BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />);
    expect(screen.getByText("2/4 closed (50%)")).toBeInTheDocument();
  });

  it("says so when a backlog has no tasks rather than reading as 0% done", () => {
    const backlogs = [makeBacklog({ id: "b1", name: "Sprint 1", dueOn: "2026-08-07" })];
    render(<BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />);
    expect(screen.getByText("No tasks")).toBeInTheDocument();
  });

  it("splits a part-done bar into a filled and a faded segment", () => {
    const backlogs = [
      makeBacklog({
        id: "b1",
        name: "Sprint 1",
        startDate: "2026-08-01",
        dueOn: "2026-08-07",
        taskCount: 2,
        closedTaskCount: 1,
      }),
    ];
    const { container } = render(
      <BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />,
    );
    // Three rectangles per row: the transparent leading offset, the done
    // segment and the remaining one.
    expect(container.querySelectorAll(".recharts-bar-rectangle")).toHaveLength(3);
  });

  it("marks today on the axis and drops the marker outside the plotted range", () => {
    const backlogs = [
      makeBacklog({ id: "b1", name: "Sprint 1", startDate: "2026-08-01", dueOn: "2026-08-07" }),
    ];
    const inRange = render(
      <BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />,
    );
    expect(inRange.container.querySelectorAll(".recharts-reference-line")).toHaveLength(1);

    const outOfRange = render(
      <BacklogTimelineSection
        projectId="p1"
        backlogs={backlogs}
        now={new Date("2027-01-01T00:00:00Z")}
      />,
    );
    expect(outOfRange.container.querySelectorAll(".recharts-reference-line")).toHaveLength(0);
  });

  it("names every bar colour in a legend, so status is never colour-only", () => {
    const backlogs = [makeBacklog({ id: "b1", name: "Sprint 1", startDate: "2026-08-01" })];
    render(<BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />);
    const legend = within(screen.getByRole("list", { name: "Bar colours" }));
    for (const label of ["Open", "Overdue", "Closed", "Remaining"]) {
      expect(legend.getByText(label)).toBeInTheDocument();
    }
  });

  // The zoom control itself is exercised in TaskTimelineSection.test.tsx; this
  // only asserts the Backlog timeline offers the same one, rather than
  // silently dropping it.
  it("offers the zoom and Today controls", () => {
    const backlogs = [
      makeBacklog({ id: "b1", name: "Sprint 1", startDate: "2026-08-01", dueOn: "2026-08-10" }),
    ];
    render(<BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />);
    const zoom = within(screen.getByRole("group", { name: "Zoom" }));
    expect(zoom.getByRole("button", { name: "Day" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Today" })).toBeEnabled();
  });

  it("lists unscheduled backlogs separately, outside the chart", () => {
    const backlogs = [
      makeBacklog({ id: "b1", name: "Sprint 1", startDate: "2026-08-01" }),
      makeBacklog({ id: "b2", name: "Someday" }),
    ];
    render(<BacklogTimelineSection projectId="p1" backlogs={backlogs} now={NOW} />);
    expect(screen.queryByRole("link", { name: "Someday" })).not.toBeInTheDocument();
    expect(screen.getByText(/Someday/)).toBeInTheDocument();
  });
});
