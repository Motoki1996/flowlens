import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { BacklogBoardSection } from "./BacklogBoardSection";

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

const urgentBacklog: Backlog = { ...backlog, id: "b2", name: "Hotfixes", priority: "urgent" };

/** card returns the board card of one backlog, found through its title link. */
function card(name: string) {
  return screen.getByRole("link", { name }).closest("li") as HTMLElement;
}

describe("BacklogBoardSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("puts each backlog in its priority's column", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog, urgentBacklog]} tasks={[]} />);

    const urgent = screen.getByRole("region", { name: "Urgent backlogs" });
    expect(within(urgent).getByRole("link", { name: "Hotfixes" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b2",
    );

    const medium = screen.getByRole("region", { name: "Medium backlogs" });
    expect(within(medium).getByRole("link", { name: "Sprint 1" })).toBeInTheDocument();

    expect(
      within(screen.getByRole("region", { name: "High backlogs" })).getByText("No backlogs."),
    ).toBeInTheDocument();
  });

  it("shows a card's task progress and planned period", () => {
    const tasks = [
      { id: "t1", backlogId: "b1", status: "closed" },
      { id: "t2", backlogId: "b1", status: "open" },
    ] as Task[];
    const scheduled = { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" };
    render(<BacklogBoardSection projectId="p1" backlogs={[scheduled]} tasks={tasks} />);

    expect(within(card("Sprint 1")).getByText("1/2 closed")).toBeInTheDocument();
    expect(within(card("Sprint 1")).getByText(/Aug 1, 2026\s*–\s*Aug 31, 2026/)).toBeInTheDocument();
  });

  it("changes a backlog's priority when its card is dropped on another column", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ ...backlog, priority: "urgent" }), { status: 200 }),
    );
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} tasks={[]} />);

    fireEvent.dragStart(card("Sprint 1"));
    fireEvent.drop(screen.getByRole("region", { name: "Urgent backlogs" }));

    // The card moves ahead of the response, so the drag reads as a drag.
    expect(
      within(screen.getByRole("region", { name: "Urgent backlogs" })).getByRole("link", {
        name: "Sprint 1",
      }),
    ).toBeInTheDocument();

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/backlogs/b1",
      expect.objectContaining({ method: "PATCH" }),
    );
    const body = JSON.parse(vi.mocked(fetch).mock.calls[0][1]?.body as string);
    expect(body).toMatchObject({ name: "Sprint 1", priority: "urgent" });
  });

  it("puts the card back and reports the error when the change fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid_priority", message: "Nope" } }), {
        status: 400,
      }),
    );
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} tasks={[]} />);

    fireEvent.dragStart(card("Sprint 1"));
    fireEvent.drop(screen.getByRole("region", { name: "Urgent backlogs" }));

    expect(await screen.findByText("Nope")).toBeInTheDocument();
    expect(
      within(screen.getByRole("region", { name: "Medium backlogs" })).getByRole("link", {
        name: "Sprint 1",
      }),
    ).toBeInTheDocument();
  });

  it("does nothing when a card is dropped on the column it came from", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} tasks={[]} />);

    fireEvent.dragStart(card("Sprint 1"));
    fireEvent.drop(screen.getByRole("region", { name: "Medium backlogs" }));

    expect(fetch).not.toHaveBeenCalled();
  });

  it("says so instead of showing every backlog as taskless when tasks failed to load", () => {
    render(<BacklogBoardSection projectId="p1" backlogs={[backlog]} tasks={[]} tasksError />);

    expect(screen.getByText("Failed to load tasks — progress is unavailable.")).toBeInTheDocument();
    expect(screen.queryByText("No tasks")).not.toBeInTheDocument();
  });
});
