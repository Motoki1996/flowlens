import { describe, it, expect, vi, beforeEach } from "vitest";
import userEvent from "@testing-library/user-event";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskBoardSection } from "./TaskBoardSection";

const refresh = vi.fn();
const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh, push }),
}));

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  startDate: null,
  dueOn: null,
  priority: "medium",
  progress: "not_started",
  defaultLinkedGitlabProjectId: null,
  baseBranch: "",
  allowedScope: "",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  forbiddenScope: "",
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const task: Task = {
  id: "t1",
  projectId: "p1",
  backlogId: "b1",
  epicId: null,
  title: "Fix login",
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
  createdByUserId: "u1",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  gitlab: null,
  aiContext: { acceptanceCriteria: "", aiContext: "", updatedAt: null },
};

const heldTask: Task = { ...task, id: "t2", title: "Site down", progress: "on_hold", backlogId: null };

/** card returns the board card of one task, found through its title link. */
function card(name: string) {
  return screen.getByRole("link", { name }).closest("li") as HTMLElement;
}

describe("TaskBoardSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    push.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("puts each task in its progress's column", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task, heldTask]} backlogs={[backlog]} />);

    const onHold = screen.getByRole("region", { name: "On hold tasks" });
    expect(within(onHold).getByRole("link", { name: "Site down" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t2",
    );

    const notStarted = screen.getByRole("region", { name: "Not started tasks" });
    expect(within(notStarted).getByRole("link", { name: "Fix login" })).toBeInTheDocument();

    expect(
      within(screen.getByRole("region", { name: "Done tasks" })).getByText("No tasks."),
    ).toBeInTheDocument();
  });

  // A card is only as wide as its column, so a long title clips at two lines
  // rather than stretching the card — and the whole of it stays reachable on
  // hover. jsdom lays nothing out, so the heights that decide "clipped" are set
  // here directly.
  it("offers a card's full title on hover once it has been clamped", async () => {
    const user = userEvent.setup();
    const title =
      "Reconcile the outbox worker with the webhook receiver before the resync";
    render(
      <TaskBoardSection
        projectId="p1"
        tasks={[{ ...task, title }]}
        backlogs={[backlog]}
      />,
    );

    const name = screen.getByRole("link", { name: title });
    Object.defineProperty(name, "scrollHeight", { value: 60, configurable: true });
    Object.defineProperty(name, "clientHeight", { value: 40, configurable: true });
    await user.hover(name);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(title);
  });

  it("names each card's backlog, falling back to Unclassified", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task, heldTask]} backlogs={[backlog]} />);

    expect(within(card("Fix login")).getByText(/Sprint 1/)).toBeInTheDocument();
    expect(within(card("Site down")).getByText(/Unclassified/)).toBeInTheDocument();
  });

  it("changes a task's progress when its card is dropped on another column", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ ...task, progress: "in_progress" }), { status: 200 }),
    );
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.dragStart(card("Fix login"));
    fireEvent.drop(screen.getByRole("region", { name: "In progress tasks" }));

    // The card moves ahead of the response, so the drag reads as a drag.
    expect(
      within(screen.getByRole("region", { name: "In progress tasks" })).getByRole("link", {
        name: "Fix login",
      }),
    ).toBeInTheDocument();

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    // Only progress travels: the PATCH must not carry status or priority.
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/tasks/t1",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({ progress: "in_progress" }),
      }),
    );
  });

  it("puts the card back and reports the error when the change fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid_progress", message: "Nope" } }), {
        status: 400,
      }),
    );
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.dragStart(card("Fix login"));
    fireEvent.drop(screen.getByRole("region", { name: "Done tasks" }));

    expect(await screen.findByText("Nope")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Not started tasks" })).getByRole("link", {
        name: "Fix login",
      }),
    ).toBeInTheDocument();
  });

  it("does nothing when a card is dropped on the column it came from", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.dragStart(card("Fix login"));
    fireEvent.drop(screen.getByRole("region", { name: "Not started tasks" }));

    expect(fetch).not.toHaveBeenCalled();
  });

  it("opens the task when the card itself is clicked, but not when one of its controls is", () => {
    render(<TaskBoardSection projectId="p1" tasks={[task]} backlogs={[backlog]} />);

    fireEvent.click(card("Fix login"));
    expect(push).toHaveBeenCalledWith("/projects/p1/tasks/t1");

    push.mockClear();
    fireEvent.click(screen.getByRole("link", { name: "Fix login" }));
    expect(push).not.toHaveBeenCalled();
  });

  it("keeps a closed task's status and priority visible on its card", () => {
    render(
      <TaskBoardSection
        projectId="p1"
        tasks={[{ ...task, status: "closed", priority: "urgent" }]}
        backlogs={[backlog]}
      />,
    );
    // The board's axis is progress, so neither of the other two may be read
    // off the column a card sits in.
    expect(within(card("Fix login")).getByText("Closed")).toBeInTheDocument();
    expect(within(card("Fix login")).getByText("Urgent")).toBeInTheDocument();
  });

  describe("Due date state (issue #148)", () => {
    // Wednesday 2026-08-05, matching lib/dashboard.test.ts's dueStatus fixture.
    const now = new Date(2026, 7, 5, 12, 0, 0);

    it("marks a card's due date Overdue, in destructive text, for a task due before today", () => {
      render(
        <TaskBoardSection
          projectId="p1"
          tasks={[{ ...task, dueOn: "2026-08-04" }]}
          backlogs={[backlog]}
          now={now}
        />,
      );

      expect(within(card("Fix login")).getByText("Overdue Aug 4, 2026")).toHaveClass(
        "text-destructive",
      );
    });

    it("shows a card's due date as Due, not Overdue, for a task due today or later", () => {
      render(
        <TaskBoardSection
          projectId="p1"
          tasks={[{ ...task, dueOn: "2026-08-05" }]}
          backlogs={[backlog]}
          now={now}
        />,
      );

      expect(within(card("Fix login")).getByText("Due Aug 5, 2026")).not.toHaveClass(
        "text-destructive",
      );
    });
  });

  describe("Label filter (issue #147)", () => {
    it("calls onLabelClick when a label badge is clicked, without opening the task", () => {
      const onLabelClick = vi.fn();
      render(
        <TaskBoardSection
          projectId="p1"
          tasks={[{ ...task, labels: ["bug"] }]}
          backlogs={[backlog]}
          onLabelClick={onLabelClick}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "bug" }));

      expect(onLabelClick).toHaveBeenCalledWith("bug");
      expect(push).not.toHaveBeenCalled();
    });

    it("marks the active label's badge as pressed", () => {
      render(
        <TaskBoardSection
          projectId="p1"
          tasks={[{ ...task, labels: ["bug", "docs"] }]}
          backlogs={[backlog]}
          activeLabel="bug"
        />,
      );

      expect(screen.getByRole("button", { name: "bug" })).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByRole("button", { name: "docs" })).toHaveAttribute("aria-pressed", "false");
    });
  });
});
