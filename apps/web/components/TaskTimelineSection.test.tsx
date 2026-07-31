import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Task, TaskDependency } from "@/types";
import { TaskTimelineSection } from "./TaskTimelineSection";

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: null,
    title: "Task",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    labels: [],
    dueOn: null,
    startDate: null,
    position: 0,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      allowedScope: "",
      forbiddenScope: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

describe("TaskTimelineSection", () => {
  it("shows a guidance message when no task has a schedule", () => {
    render(<TaskTimelineSection tasks={[makeTask({})]} dependencies={[]} />);
    expect(
      screen.getByText("No scheduled tasks yet. Set a start date or due date on a task to see it on the timeline."),
    ).toBeInTheDocument();
  });

  it("renders a bar row for each scheduled task", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", dueOn: "2026-08-03" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04", dueOn: "2026-08-10" }),
    ];
    render(<TaskTimelineSection tasks={tasks} dependencies={[]} />);
    expect(screen.getByRole("link", { name: "Design" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Build" })).toBeInTheDocument();
  });

  it("shows the closed/total progress ratio", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01", status: "closed" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04", status: "open" }),
    ];
    render(<TaskTimelineSection tasks={tasks} dependencies={[]} />);
    expect(screen.getByText("1/2 closed (50%)")).toBeInTheDocument();
  });

  it("labels a task with its predecessor's title", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-08-04" }),
    ];
    const dependencies: TaskDependency[] = [
      { id: "d1", predecessorTaskId: "t1", successorTaskId: "t2", createdAt: "2026-01-01T00:00:00Z" },
    ];
    render(<TaskTimelineSection tasks={tasks} dependencies={dependencies} />);
    expect(screen.getByText("After: Design")).toBeInTheDocument();
  });

  it("lists unscheduled tasks separately, outside the chart", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Design", startDate: "2026-08-01" }),
      makeTask({ id: "t2", title: "No dates yet" }),
    ];
    render(<TaskTimelineSection tasks={tasks} dependencies={[]} />);
    expect(screen.queryByRole("link", { name: "No dates yet" })).not.toBeInTheDocument();
    expect(screen.getByText(/No dates yet/)).toBeInTheDocument();
  });
});
