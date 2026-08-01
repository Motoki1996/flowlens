import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskListSection } from "./TaskListSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

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

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  position: 0,
  startDate: null,
  dueOn: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const otherBacklog: Backlog = {
  ...backlog,
  id: "b2",
  name: "Icebox",
  position: 1,
};

describe("TaskListSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows an empty state with zero tasks", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
    expect(screen.getByText("No tasks yet.")).toBeInTheDocument();
  });

  it("groups tasks with no backlog under Unclassified", () => {
    const tasks = [makeTask({ id: "t1", title: "Unfiled task", backlogId: null })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
    expect(screen.getByText("Unclassified (1)")).toBeInTheDocument();
    expect(screen.getByText("Unfiled task")).toBeInTheDocument();
  });

  it("groups tasks by backlog, ordered before the Unclassified group", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Filed task", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Unfiled task", backlogId: null }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings).toEqual(["Sprint 1 (1)", "Unclassified (1)"]);
  });

  it("includes closed tasks by default", () => {
    const tasks = [makeTask({ id: "t1", title: "Closed task", status: "closed" })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
    expect(screen.getByText("Closed task")).toBeInTheDocument();
    expect(screen.getByText("Closed", { selector: "span" })).toBeInTheDocument();
  });

  it("shows a load error", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} error />);
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });

  it("assigns selected unclassified tasks to a backlog in bulk", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    const tasks = [
      makeTask({ id: "t1", title: "Unfiled task 1", backlogId: null }),
      makeTask({ id: "t2", title: "Unfiled task 2", backlogId: null }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);

    fireEvent.click(screen.getByRole("checkbox", { name: "Select Unfiled task 1" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select Unfiled task 2" }));
    expect(screen.getByText("2 selected")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("combobox", { name: "Backlog to assign" }));
    fireEvent.click(await screen.findByRole("option", { name: "Sprint 1" }));
    fireEvent.click(screen.getByRole("button", { name: "Assign to backlog" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: "b1" }) }),
    );
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t2/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: "b1" }) }),
    );
  });

  it("narrows the list to the backlog picked in the filter", async () => {
    const tasks = [
      makeTask({ id: "t1", title: "Sprint task", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Icebox task", backlogId: "b2" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);

    fireEvent.click(screen.getByRole("combobox", { name: "Backlog" }));
    fireEvent.click(await screen.findByRole("option", { name: "Icebox" }));

    expect(screen.getByText("Icebox task")).toBeInTheDocument();
    expect(screen.queryByText("Sprint task")).not.toBeInTheDocument();
  });

  it("does not offer selection for tasks already in a backlog", () => {
    const tasks = [makeTask({ id: "t1", title: "Filed task", backlogId: "b1" })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("switches to the timeline view mode and back, keeping the filters in place", async () => {
    const tasks = [makeTask({ id: "t1", title: "Scheduled task", startDate: "2026-08-01" })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    // The timeline is loaded on demand, so the link only appears once its chunk resolves.
    expect(
      await screen.findByRole("link", { name: "Scheduled task" }, { timeout: 15000 }),
    ).toBeInTheDocument();
    // The filters belong to the collection, not to the list presentation, so
    // they stay put — a view switch that reflowed the header would move the
    // buttons out from under the pointer.
    expect(screen.getByRole("combobox", { name: "Status" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "List" }));
    expect(screen.getByRole("combobox", { name: "Status" })).toBeInTheDocument();
  });

  it("narrows the timeline to the backlog picked in the filter", async () => {
    const tasks = [
      makeTask({ id: "t1", title: "Sprint task", backlogId: "b1", startDate: "2026-08-01" }),
      makeTask({ id: "t2", title: "Icebox task", backlogId: "b2", startDate: "2026-08-02" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);

    fireEvent.click(screen.getByRole("combobox", { name: "Backlog" }));
    fireEvent.click(await screen.findByRole("option", { name: "Icebox" }));
    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));

    expect(
      await screen.findByRole("link", { name: "Icebox task" }, { timeout: 15000 }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Sprint task" })).not.toBeInTheDocument();
  });

  it("reports an empty filter result the same way in either view mode", async () => {
    const tasks = [makeTask({ id: "t1", title: "Sprint task", backlogId: "b1" })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);

    fireEvent.click(screen.getByRole("combobox", { name: "Backlog" }));
    fireEvent.click(await screen.findByRole("option", { name: "Icebox" }));
    expect(screen.getByText("No tasks match the current filters.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    expect(screen.getByText("No tasks match the current filters.")).toBeInTheDocument();
  });

  it("offers task creation even when the project has no tasks yet", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
    expect(screen.getByRole("button", { name: "New task" })).toBeInTheDocument();
  });

  it("requires a title before posting a new task", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    expect(screen.getByText("Task title is required.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("creates a task, sending the day picked in the calendar as RFC3339", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-08-10T09:00:00Z"));
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }));
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[backlog]} />);

    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Fix bug" } });

    // The calendar opens on the current month, so August 15th is one click
    // away. Day buttons are named by react-day-picker's own aria-label.
    fireEvent.click(screen.getByLabelText("Start date"));
    fireEvent.click(await screen.findByRole("button", { name: /August 15th, 2026/ }));
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/projects/p1/tasks",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          title: "Fix bug",
          description: "",
          backlogId: null,
          startDate: "2026-08-15T00:00:00Z",
          dueOn: null,
        }),
      }),
    );
    expect(screen.queryByRole("form", { name: "New task" })).not.toBeInTheDocument();
  });

  it("creates a task in the backlog picked from the combobox", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 201 }));
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[backlog, otherBacklog]} />);

    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Filed task" } });
    fireEvent.click(screen.getByLabelText("Backlog"));
    fireEvent.click(await screen.findByRole("option", { name: "Sprint 1" }));
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/projects/p1/tasks",
      expect.objectContaining({ body: expect.stringContaining('"backlogId":"b1"') }),
    );
  });

  it("narrows the backlog combobox to the typed text", async () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[backlog, otherBacklog]} />);

    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.click(screen.getByLabelText("Backlog"));
    expect(await screen.findByRole("option", { name: "Icebox" })).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("Search backlogs…"), {
      target: { value: "sprint" },
    });

    await waitFor(() => expect(screen.queryByRole("option", { name: "Icebox" })).toBeNull());
    expect(screen.getByRole("option", { name: "Sprint 1" })).toBeInTheDocument();
  });

  it("keeps the form open and shows the API error when creation fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid_title", message: "title is required" } }), {
        status: 400,
      }),
    );
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Fix bug" } });
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    expect(await screen.findByText("title is required")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
    expect(screen.getByRole("form", { name: "New task" })).toBeInTheDocument();
  });

  it("shows a sync badge per task row", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Local task", gitlab: null }),
      makeTask({
        id: "t2",
        title: "Failed task",
        gitlab: {
          syncStatus: "failed",
          lastError: "gitlab rejected the update",
          lastSyncedAt: null,
          issueIid: null,
          webUrl: "",
        },
      }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
    expect(screen.getByText("Local only")).toBeInTheDocument();
    expect(screen.getByText("Sync failed")).toBeInTheDocument();
  });
});
