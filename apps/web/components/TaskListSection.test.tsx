import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskListSection } from "./TaskListSection";

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

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  position: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("TaskListSection", () => {
  it("shows an empty state with zero tasks", () => {
    render(<TaskListSection tasks={[]} backlogs={[]} />);
    expect(screen.getByText("No tasks yet.")).toBeInTheDocument();
  });

  it("groups tasks with no backlog under 未分類", () => {
    const tasks = [makeTask({ id: "t1", title: "Unfiled task", backlogId: null })];
    render(<TaskListSection tasks={tasks} backlogs={[backlog]} />);
    expect(screen.getByText("未分類 (1)")).toBeInTheDocument();
    expect(screen.getByText("Unfiled task")).toBeInTheDocument();
  });

  it("groups tasks by backlog, ordered before the 未分類 group", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Filed task", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Unfiled task", backlogId: null }),
    ];
    render(<TaskListSection tasks={tasks} backlogs={[backlog]} />);
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings).toEqual(["Sprint 1 (1)", "未分類 (1)"]);
  });

  it("includes closed tasks by default", () => {
    const tasks = [makeTask({ id: "t1", title: "Closed task", status: "closed" })];
    render(<TaskListSection tasks={tasks} backlogs={[]} />);
    expect(screen.getByText("Closed task")).toBeInTheDocument();
    expect(screen.getByText("Closed", { selector: "span" })).toBeInTheDocument();
  });

  it("shows a load error", () => {
    render(<TaskListSection tasks={[]} backlogs={[]} error />);
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });
});
