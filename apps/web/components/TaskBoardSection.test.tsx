import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskBoardSection } from "./TaskBoardSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  position: 0,
  startDate: null,
  dueOn: null,
  priority: "medium",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const task: Task = {
  id: "t1",
  projectId: "p1",
  backlogId: "b1",
  title: "Fix login",
  description: "",
  status: "open",
  closedAt: null,
  assigneeGitlabUserId: null,
  assigneeGitlabUsername: "",
  labels: [],
  dueOn: null,
  startDate: null,
  priority: "medium",
  position: 0,
  createdByUserId: "u1",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  gitlab: null,
  aiContext: { acceptanceCriteria: "", aiContext: "", allowedScope: "", forbiddenScope: "", updatedAt: null },
};

const urgentTask: Task = { ...task, id: "t2", title: "Site down", priority: "urgent", backlogId: null };

/** card returns the board card of one task, found through its title link. */
function card(name: string) {
  return screen.getByRole("link", { name }).closest("li") as HTMLElement;
}

describe("TaskBoardSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("puts each task in its priority's column", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task, urgentTask]} backlogs={[backlog]} />);

    const urgent = screen.getByRole("region", { name: "Urgent tasks" });
    expect(within(urgent).getByRole("link", { name: "Site down" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t2",
    );

    const medium = screen.getByRole("region", { name: "Medium tasks" });
    expect(within(medium).getByRole("link", { name: "Fix login" })).toBeInTheDocument();

    expect(
      within(screen.getByRole("region", { name: "Low tasks" })).getByText("No tasks."),
    ).toBeInTheDocument();
  });

  it("names each card's backlog, falling back to Unclassified", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task, urgentTask]} backlogs={[backlog]} />);

    expect(within(card("Fix login")).getByText(/Sprint 1/)).toBeInTheDocument();
    expect(within(card("Site down")).getByText(/Unclassified/)).toBeInTheDocument();
  });

  it("changes a task's priority when its card is dropped on another column", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ ...task, priority: "high" }), { status: 200 }),
    );
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.dragStart(card("Fix login"));
    fireEvent.drop(screen.getByRole("region", { name: "High tasks" }));

    // The card moves ahead of the response, so the drag reads as a drag.
    expect(
      within(screen.getByRole("region", { name: "High tasks" })).getByRole("link", {
        name: "Fix login",
      }),
    ).toBeInTheDocument();

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1",
      expect.objectContaining({ method: "PATCH", body: JSON.stringify({ priority: "high" }) }),
    );
  });

  it("puts the card back and reports the error when the change fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid_priority", message: "Nope" } }), {
        status: 400,
      }),
    );
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.dragStart(card("Fix login"));
    fireEvent.drop(screen.getByRole("region", { name: "Urgent tasks" }));

    expect(await screen.findByText("Nope")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Medium tasks" })).getByRole("link", {
        name: "Fix login",
      }),
    ).toBeInTheDocument();
  });

  it("does nothing when a card is dropped on the column it came from", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.dragStart(card("Fix login"));
    fireEvent.drop(screen.getByRole("region", { name: "Medium tasks" }));

    expect(fetch).not.toHaveBeenCalled();
  });

  it("keeps a closed task's status visible on its card", () => {
    render(
      <TaskBoardSection
        projectId="p1"
        tasks={[{ ...task, status: "closed" }]}
        backlogs={[backlog]}
      />,
    );
    expect(within(card("Fix login")).getByText("Closed")).toBeInTheDocument();
  });
});
