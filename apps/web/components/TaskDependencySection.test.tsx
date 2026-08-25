import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Task, TaskDependency } from "@/types";
import { TaskDependencySection } from "./TaskDependencySection";

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: null,
    epicId: null,
    title: "Fix the bug",
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

const task = makeTask({});
const tasks = [task, makeTask({ id: "t2", title: "Design the fix" }), makeTask({ id: "t3", title: "Ship the fix" })];

const predecessor: TaskDependency = {
  id: "d1",
  predecessorTaskId: "t2",
  successorTaskId: "t1",
  createdAt: "2026-01-01T00:00:00Z",
};

const successor: TaskDependency = {
  id: "d2",
  predecessorTaskId: "t1",
  successorTaskId: "t3",
  createdAt: "2026-01-01T00:00:00Z",
};

describe("TaskDependencySection", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("lists predecessors and successors, each linking to its task", () => {
    render(<TaskDependencySection task={task} tasks={tasks} dependencies={[predecessor, successor]} />);

    expect(screen.getByRole("link", { name: "Design the fix" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t2",
    );
    expect(screen.getByRole("link", { name: "Ship the fix" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t3",
    );
  });

  it("adds the picked task as a predecessor of this task", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(predecessor), { status: 201 }));

    render(<TaskDependencySection task={task} tasks={tasks} dependencies={[]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Add predecessors" }));
    fireEvent.click(await screen.findByRole("option", { name: "Design the fix" }));

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "Design the fix" })).toBeInTheDocument(),
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/projects/p1/task-dependencies",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ predecessorTaskId: "t2", successorTaskId: "t1" }),
      }),
    );
  });

  it("adds the picked task as a successor, with this task as the predecessor", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(successor), { status: 201 }));

    render(<TaskDependencySection task={task} tasks={tasks} dependencies={[]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Add successors" }));
    fireEvent.click(await screen.findByRole("option", { name: "Ship the fix" }));

    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/projects/p1/task-dependencies",
        expect.objectContaining({
          body: JSON.stringify({ predecessorTaskId: "t1", successorTaskId: "t3" }),
        }),
      ),
    );
  });

  it("removes a dependency", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));

    render(<TaskDependencySection task={task} tasks={tasks} dependencies={[predecessor]} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove Design the fix from predecessors" }));

    await waitFor(() =>
      expect(screen.queryByRole("link", { name: "Design the fix" })).not.toBeInTheDocument(),
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/task-dependencies/d1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("surfaces the API's cycle rejection", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          error: { code: "dependency_cycle", message: "that dependency would create a cycle" },
        }),
        { status: 400 },
      ),
    );

    render(<TaskDependencySection task={task} tasks={tasks} dependencies={[]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Add predecessors" }));
    fireEvent.click(await screen.findByRole("option", { name: "Design the fix" }));

    expect(await screen.findByText("that dependency would create a cycle")).toBeInTheDocument();
  });

  it("offers neither the task itself nor one already linked in that direction", async () => {
    render(<TaskDependencySection task={task} tasks={tasks} dependencies={[predecessor]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Add predecessors" }));

    expect(await screen.findByRole("option", { name: "Ship the fix" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Fix the bug" })).not.toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "Design the fix" })).not.toBeInTheDocument();
  });
});
