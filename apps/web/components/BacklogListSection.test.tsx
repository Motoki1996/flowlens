import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { BacklogListSection } from "./BacklogListSection";

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

describe("BacklogListSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows an empty state with zero backlogs", () => {
    render(<BacklogListSection projectId="p1" backlogs={[]} tasks={[]} />);
    expect(screen.getByText("No backlogs yet.")).toBeInTheDocument();
  });

  it("lists backlogs with their task count and a link to the single view", () => {
    const tasks: Task[] = [];
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} tasks={tasks} />);
    const link = screen.getByRole("link", { name: /Sprint 1/ });
    expect(link).toHaveAttribute("href", "/projects/p1/backlogs/b1");
    expect(link).toHaveTextContent("(0)");
  });

  it("creates a new backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ ...backlog, id: "b2" }), { status: 201 }));
    render(<BacklogListSection projectId="p1" backlogs={[]} tasks={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "New backlog" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Sprint 2" } });
    fireEvent.click(screen.getByRole("button", { name: "Create backlog" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/projects/p1/backlogs",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("renames a backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ ...backlog, name: "Renamed" }), { status: 200 }));
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} tasks={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/backlogs/b1",
      expect.objectContaining({ method: "PATCH" }),
    );
  });

  it("sends the edited schedule with the rest of the backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(backlog), { status: 200 }));
    const scheduled = { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" };
    render(<BacklogListSection projectId="p1" backlogs={[scheduled]} tasks={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    const body = JSON.parse(vi.mocked(fetch).mock.calls[0][1]?.body as string);
    expect(body).toMatchObject({
      name: "Sprint 1",
      startDate: "2026-08-01T00:00:00Z",
      dueOn: "2026-08-31T00:00:00Z",
    });
  });

  it("shows a backlog's planned period in the list row", () => {
    const scheduled = { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" };
    render(<BacklogListSection projectId="p1" backlogs={[scheduled]} tasks={[]} />);
    expect(screen.getByText(/Aug 1, 2026\s*–\s*Aug 31, 2026/)).toBeInTheDocument();
  });

  it("switches to the timeline view mode and back", async () => {
    const scheduled = { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" };
    render(<BacklogListSection projectId="p1" backlogs={[scheduled]} tasks={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    // The timeline is loaded on demand, so the legend only appears once its
    // chunk resolves.
    expect(
      await screen.findByRole("list", { name: "Bar colours" }, { timeout: 15000 }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "List" }));
    expect(screen.queryByRole("list", { name: "Bar colours" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  });

  it("requires a confirmation step before deleting, and explains where tasks go", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} tasks={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(
      screen.getByText("Its tasks will move to Unclassified. Delete this backlog?"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/backlogs/b1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });
});
