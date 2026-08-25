import { describe, it, expect, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Task, TaskDependency } from "@/types";
import { TaskTimelineSection } from "./TaskTimelineSection";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

// The chart's date math is covered in lib/timeline.test.ts; these tests cover
// what the DOM must offer regardless of how the bars are drawn — navigation to
// each task, the progress summary, the legend that keeps status readable
// without relying on colour, and the tasks the chart cannot show.
const NOW = new Date("2026-08-05T00:00:00Z");

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: null,
    epicId: null,
    title: "Task",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
    size: "m",
    designStartedAt: null,
    implementationStartedAt: null,
    position: 0,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

describe("TaskTimelineSection", () => {
  it("shows a guidance message when no task has a schedule", () => {
    render(<TaskTimelineSection projectId="p1" tasks={[makeTask({})]} dependencies={[]} now={NOW} />);
    expect(
      screen.getByText("No scheduled tasks yet. Set a start date or due date on a task to see it on the timeline."),
    ).toBeInTheDocument();
  });

  it("links every scheduled task to its single view", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", dueOn: "2026-08-03" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04", dueOn: "2026-08-10" }),
    ];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    expect(screen.getByRole("link", { name: "Design" })).toHaveAttribute("href", "/projects/p1/tasks/t1");
    expect(screen.getByRole("link", { name: "Build" })).toHaveAttribute("href", "/projects/p1/tasks/t2");
  });

  it("orders the task names by start date, matching the bars", () => {
    const tasks = [
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04" }),
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" }),
    ];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    expect(screen.getAllByRole("link").map((el) => el.textContent)).toEqual(["Design", "Build"]);
  });

  it("plots a bar per scheduled task and marks today on the axis", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", dueOn: "2026-08-03" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04", dueOn: "2026-08-10" }),
    ];
    const { container } = render(
      <TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />,
    );
    // Two rectangles per row: the transparent leading offset and the visible bar.
    expect(container.querySelectorAll(".recharts-bar-rectangle")).toHaveLength(4);
    expect(container.querySelectorAll(".recharts-reference-line")).toHaveLength(1);
  });

  it("omits the today marker when today falls outside the plotted range", () => {
    const tasks = [makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" })];
    const { container } = render(
      <TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={new Date("2027-01-01T00:00:00Z")} />,
    );
    expect(container.querySelectorAll(".recharts-reference-line")).toHaveLength(0);
  });

  it("shows the closed/total progress ratio", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", status: "closed" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04", status: "open" }),
    ];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    expect(screen.getByText("1/2 closed (50%)")).toBeInTheDocument();
  });

  it("names every bar colour in a legend, so status is never colour-only", () => {
    const tasks = [makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" })];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    const legend = within(screen.getByRole("list", { name: "Bar colours" }));
    for (const label of ["Open", "Overdue", "Closed"]) {
      expect(legend.getByText(label)).toBeInTheDocument();
    }
  });

  it("opens at the zoom the span calls for, and lets the reader change it", async () => {
    const user = userEvent.setup();
    // A three-day sprint is read day by day; the reader can still pull back.
    const tasks = [makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", dueOn: "2026-08-03" })];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    const zoom = within(screen.getByRole("group", { name: "Zoom" }));
    expect(zoom.getByRole("button", { name: "Day" })).toHaveAttribute("aria-pressed", "true");

    await user.click(zoom.getByRole("button", { name: "Month" }));
    expect(zoom.getByRole("button", { name: "Month" })).toHaveAttribute("aria-pressed", "true");
    expect(zoom.getByRole("button", { name: "Day" })).toHaveAttribute("aria-pressed", "false");
  });

  it("opens a long range zoomed out rather than at daily detail", () => {
    const tasks = [makeTask({ id: "t1", title: "Rollout", startDate: "2026-01-01", dueOn: "2026-12-31" })];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    const zoom = within(screen.getByRole("group", { name: "Zoom" }));
    expect(zoom.getByRole("button", { name: "Month" })).toHaveAttribute("aria-pressed", "true");
  });

  it("disables the Today button when today is outside the plotted range", () => {
    const tasks = [makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", dueOn: "2026-08-10" })];
    const { rerender } = render(
      <TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />,
    );
    expect(screen.getByRole("button", { name: "Today" })).toBeEnabled();

    rerender(
      <TaskTimelineSection
        projectId="p1"
        tasks={tasks}
        dependencies={[]}
        now={new Date("2027-01-01T00:00:00Z")}
      />,
    );
    expect(screen.getByRole("button", { name: "Today" })).toBeDisabled();
  });

  it("labels a task with its predecessor's title", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04" }),
    ];
    const dependencies: TaskDependency[] = [
      { id: "d1", predecessorTaskId: "t1", successorTaskId: "t2", createdAt: "2026-01-01T00:00:00Z" },
    ];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={dependencies} now={NOW} />);
    expect(screen.getByText("After: Design")).toBeInTheDocument();
  });

  // The name column is narrow, so it carries the title plus only what changes
  // which row you look at first. Priority and progress in full belong to the
  // bar's tooltip (see GanttChart.test.tsx).
  it("flags a high or urgent priority beside the name, and stays quiet at the default", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", priority: "urgent" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04", priority: "medium" }),
    ];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    expect(screen.getByText("Urgent")).toBeInTheDocument();
    expect(screen.queryByText("Medium")).not.toBeInTheDocument();
    expect(screen.queryByText("Not started")).not.toBeInTheDocument();
  });

  it("lists unscheduled tasks separately, outside the chart", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" }),
      makeTask({ id: "t2", title: "No dates yet" }),
    ];
    render(<TaskTimelineSection projectId="p1" tasks={tasks} dependencies={[]} now={NOW} />);
    expect(screen.queryByRole("link", { name: "No dates yet" })).not.toBeInTheDocument();
    expect(screen.getByText(/No dates yet/)).toBeInTheDocument();
  });
});
