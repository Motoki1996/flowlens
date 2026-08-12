import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { GanttTooltip } from "./GanttChart";
import type { GanttRow } from "@/lib/timeline";

// The tooltip is rendered directly rather than by hovering a bar: recharts
// mounts it from pointer geometry an SVG doesn't have in jsdom. What matters
// here is the payload it states — priority and progress moved off the name
// column onto the tooltip, so this is the only place they are fully readable.
function makeRow(overrides: Partial<GanttRow> = {}): GanttRow {
  return {
    id: "t1",
    title: "Design",
    priority: "medium",
    progress: "in_progress",
    state: "open",
    start: new Date("2026-08-01T00:00:00Z"),
    end: new Date("2026-08-03T00:00:00Z"),
    offset: 0,
    duration: 2 * 24 * 60 * 60 * 1000,
    ...overrides,
  };
}

describe("GanttTooltip", () => {
  it("states the row's priority and progress", () => {
    render(<GanttTooltip active payload={[{ payload: makeRow({ priority: "urgent" }) }]} />);
    expect(screen.getByText("Urgent priority · In progress")).toBeInTheDocument();
  });

  it("renders nothing when no bar is hovered", () => {
    const { container } = render(<GanttTooltip payload={[{ payload: makeRow() }]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
