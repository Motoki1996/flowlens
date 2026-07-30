import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { BacklogDetail } from "./BacklogDetail";

const project = { id: "p1", name: "Alpha" };

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "The first sprint",
  position: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-02T00:00:00Z",
};

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    title: "Fix the bug",
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

describe("BacklogDetail", () => {
  it("shows identity, attributes and a link back to the project", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(screen.getByRole("heading", { name: "Sprint 1" })).toBeInTheDocument();
    expect(screen.getByText("The first sprint")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "← Alpha" })).toHaveAttribute("href", "/projects/p1");
  });

  it("shows an empty state with no tasks", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(screen.getByText("No tasks in this backlog yet.")).toBeInTheDocument();
  });

  it("lists the backlog's tasks", () => {
    const tasks = [makeTask({ id: "t1", title: "Fix the bug" }), makeTask({ id: "t2", title: "Write docs" })];
    render(<BacklogDetail backlog={backlog} project={project} tasks={tasks} />);
    expect(screen.getByRole("link", { name: /Fix the bug/ })).toHaveAttribute("href", "/tasks/t1");
    expect(screen.getByRole("link", { name: /Write docs/ })).toHaveAttribute("href", "/tasks/t2");
  });

  it("shows a load error", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} tasksError />);
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });
});
