import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { Backlog } from "@/types";
import { BacklogBoardSection } from "./BacklogBoardSection";

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
  status: "open" as const,
  closedAt: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const heldBacklog: Backlog = { ...backlog, id: "b2", name: "Hotfixes", progress: "on_hold" };

/** card returns the board card of one backlog, found through its title link. */
function card(name: string) {
  return screen.getByRole("link", { name }).closest("li") as HTMLElement;
}

describe("BacklogBoardSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    push.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("puts each backlog in its progress's column", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog, heldBacklog]} />);

    const onHold = screen.getByRole("region", { name: "On hold backlogs" });
    expect(within(onHold).getByRole("link", { name: "Hotfixes" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b2",
    );

    const notStarted = screen.getByRole("region", { name: "Not started backlogs" });
    expect(within(notStarted).getByRole("link", { name: "Sprint 1" })).toBeInTheDocument();

    expect(
      within(screen.getByRole("region", { name: "Done backlogs" })).getByText("No backlogs."),
    ).toBeInTheDocument();
  });

  it("says so when a backlog has no tasks rather than reading as 0% done", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} />);
    expect(within(card("Sprint 1")).getByText("No tasks")).toBeInTheDocument();
  });

  it("shows a card's closed-task ratio and planned period", () => {
    const scheduled = {
      ...backlog,
      startDate: "2026-08-01T00:00:00Z",
      dueOn: "2026-08-31T00:00:00Z",
      taskCount: 2,
      closedTaskCount: 1,
      status: "open" as const,
      closedAt: null,
    };
    render(<BacklogBoardSection projectId="p1" backlogs={[scheduled]} />);

    expect(within(card("Sprint 1")).getByText("1/2 closed")).toBeInTheDocument();
    expect(within(card("Sprint 1")).getByText(/Aug 1, 2026\s*–\s*Aug 31, 2026/)).toBeInTheDocument();
  });

  it("changes a backlog's progress when its card is dropped on another column", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ ...backlog, progress: "in_progress" }), { status: 200 }),
    );
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} />);

    fireEvent.dragStart(card("Sprint 1"));
    fireEvent.drop(screen.getByRole("region", { name: "In progress backlogs" }));

    // The card moves ahead of the response, so the drag reads as a drag.
    expect(
      within(screen.getByRole("region", { name: "In progress backlogs" })).getByRole("link", {
        name: "Sprint 1",
      }),
    ).toBeInTheDocument();

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/backlogs/b1",
      expect.objectContaining({ method: "PATCH" }),
    );
    // The backlog PATCH overwrites every editable column, so the body has to
    // echo the priority back unchanged rather than dropping it.
    const body = JSON.parse(vi.mocked(fetch).mock.calls[0][1]?.body as string);
    expect(body).toMatchObject({ name: "Sprint 1", priority: "medium", progress: "in_progress" });
  });

  it("puts the card back and reports the error when the change fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid_progress", message: "Nope" } }), {
        status: 400,
      }),
    );
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} />);

    fireEvent.dragStart(card("Sprint 1"));
    fireEvent.drop(screen.getByRole("region", { name: "Done backlogs" }));

    expect(await screen.findByText("Nope")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Not started backlogs" })).getByRole("link", {
        name: "Sprint 1",
      }),
    ).toBeInTheDocument();
  });

  it("does nothing when a card is dropped on the column it came from", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} />);

    fireEvent.dragStart(card("Sprint 1"));
    fireEvent.drop(screen.getByRole("region", { name: "Not started backlogs" }));

    expect(fetch).not.toHaveBeenCalled();
  });

  it("opens the backlog when the card itself is clicked, but not when one of its controls is", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} />);

    fireEvent.click(card("Sprint 1"));
    expect(push).toHaveBeenCalledWith("/projects/p1/backlogs/b1");

    push.mockClear();
    // "View tasks" is a link of its own, so it must not be swallowed by the
    // card's own navigation.
    fireEvent.click(screen.getByRole("link", { name: "View tasks" }));
    expect(push).not.toHaveBeenCalled();
  });
});
