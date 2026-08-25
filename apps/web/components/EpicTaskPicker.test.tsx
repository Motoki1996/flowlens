import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { Task } from "@/types";
import { EpicTaskPicker } from "./EpicTaskPicker";

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    epicId: null,
    title: "Build the list screen",
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
    aiContext: { acceptanceCriteria: "", aiContext: "", updatedAt: null },
    ...overrides,
  };
}

describe("EpicTaskPicker", () => {
  const tasks = [
    makeTask({ id: "t1", title: "Free task" }),
    makeTask({ id: "t2", title: "Already ours", epicId: "e1" }),
    makeTask({ id: "t3", title: "In another epic", epicId: "e2" }),
    makeTask({ id: "t4", title: "Another backlog", backlogId: "b2" }),
  ];

  // The candidates are what makes this safe to use: it can only ever move a
  // task that is free to move.
  it("offers this backlog's free tasks and the epic's own, nothing else", () => {
    render(
      <EpicTaskPicker
        id="picker"
        tasks={tasks}
        backlogId="b1"
        epicId="e1"
        value={["t2"]}
        onChange={vi.fn()}
      />,
    );

    expect(screen.getByLabelText("Free task")).not.toBeChecked();
    expect(screen.getByLabelText("Already ours")).toBeChecked();
    // Taking a task out of another epic is a decision to make on that task.
    expect(screen.queryByLabelText("In another epic")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Another backlog")).not.toBeInTheDocument();
  });

  it("reports the whole set on each toggle", () => {
    const onChange = vi.fn();
    render(
      <EpicTaskPicker
        id="picker"
        tasks={tasks}
        backlogId="b1"
        epicId="e1"
        value={["t2"]}
        onChange={onChange}
      />,
    );

    fireEvent.click(screen.getByLabelText("Free task"));
    expect(onChange).toHaveBeenCalledWith(["t2", "t1"]);

    onChange.mockClear();
    fireEvent.click(screen.getByLabelText("Already ours"));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  // An epic outside a backlog has nowhere to draw from: filing a task under
  // it would have no backlog to move the task to.
  it("says so rather than showing an empty list when the epic has no backlog", () => {
    render(
      <EpicTaskPicker id="picker" tasks={tasks} backlogId={null} value={[]} onChange={vi.fn()} />,
    );
    expect(screen.getByText(/File it in a backlog first/)).toBeInTheDocument();
  });

  it("explains an empty candidate list", () => {
    render(
      <EpicTaskPicker
        id="picker"
        tasks={[makeTask({ id: "t3", title: "In another epic", epicId: "e2" })]}
        backlogId="b1"
        epicId="e1"
        value={[]}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/No free tasks in this backlog/)).toBeInTheDocument();
  });
});
