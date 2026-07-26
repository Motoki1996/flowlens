import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskDetail } from "./TaskDetail";

const project = { id: "p1", name: "Alpha" };
const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  position: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    title: "Fix the bug",
    description: "Details about the bug.",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "octocat",
    labels: ["bug"],
    dueOn: "2026-02-01",
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

describe("TaskDetail", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows identity, attributes and a link back to the project", () => {
    render(<TaskDetail task={makeTask({})} project={project} backlog={backlog} />);
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(screen.getByText("octocat")).toBeInTheDocument();
    expect(screen.getByText("Sprint 1")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "← Alpha" })).toHaveAttribute("href", "/projects/p1");
  });

  it("shows 未分類 when the task has no backlog", () => {
    render(<TaskDetail task={makeTask({ backlogId: null })} project={project} backlog={null} />);
    expect(screen.getByText("未分類")).toBeInTheDocument();
  });

  it("closes an open task", async () => {
    const closed = makeTask({ status: "closed", closedAt: "2026-01-05T00:00:00Z" });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(closed), { status: 200 }));

    render(<TaskDetail task={makeTask({})} project={project} backlog={backlog} />);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Reopen" })).toBeInTheDocument());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1/close",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("reopens a closed task", async () => {
    const reopened = makeTask({ status: "open", closedAt: null });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(reopened), { status: 200 }));

    render(<TaskDetail task={makeTask({ status: "closed" })} project={project} backlog={backlog} />);
    fireEvent.click(screen.getByRole("button", { name: "Reopen" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument());
  });
});
