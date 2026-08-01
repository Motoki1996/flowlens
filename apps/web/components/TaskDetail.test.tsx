import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskDetail } from "./TaskDetail";

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

describe("TaskDetail", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows identity and attributes", () => {
    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(screen.getByText("octocat")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Sprint 1");
  });

  it("shows Unclassified when the task has no backlog", () => {
    render(<TaskDetail task={makeTask({ backlogId: null })} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Unclassified");
  });

  it("closes an open task", async () => {
    const closed = makeTask({ status: "closed", closedAt: "2026-01-05T00:00:00Z" });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(closed), { status: 200 }));

    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
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

    render(<TaskDetail task={makeTask({ status: "closed" })} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Reopen" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument());
  });

  it("assigns the task to a different backlog", async () => {
    const otherBacklog: Backlog = { ...backlog, id: "b2", name: "Sprint 2" };
    const reassigned = makeTask({ backlogId: "b2" });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(reassigned), { status: 200 }));

    render(<TaskDetail task={makeTask({})} backlogs={[backlog, otherBacklog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Backlog" }));
    fireEvent.click(await screen.findByRole("option", { name: "Sprint 2" }));

    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Sprint 2"),
    );
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: "b2" }) }),
    );
  });

  it("unassigns the task back to Unclassified", async () => {
    const unassigned = makeTask({ backlogId: null });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(unassigned), { status: 200 }));

    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "Backlog" }));
    fireEvent.click(await screen.findByRole("option", { name: "Unclassified" }));

    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Unclassified"),
    );
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: null }) }),
    );
  });

  it("edits the task in place and shows the saved values", async () => {
    const saved = makeTask({ title: "Fix the bug properly", labels: ["bug", "urgent"] });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(saved), { status: 200 }));

    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit task" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Fix the bug properly" } });
    fireEvent.change(screen.getByLabelText("Labels"), { target: { value: "bug, urgent" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Fix the bug properly" })).toBeInTheDocument(),
    );
    expect(screen.getByText("urgent")).toBeInTheDocument();

    const [url, init] = vi.mocked(fetch).mock.calls[0] as [string, RequestInit];
    expect(url).toBe("http://localhost:8080/api/v1/tasks/t1");
    expect(init.method).toBe("PATCH");
    // Only the fields the form shows are sent: position is absent from the
    // body, so the API leaves it alone rather than resetting it.
    expect(JSON.parse(init.body as string)).toEqual({
      title: "Fix the bug properly",
      description: "Details about the bug.",
      backlogId: "b1",
      assigneeGitlabUsername: "octocat",
      labels: ["bug", "urgent"],
      startDate: null,
      dueOn: "2026-02-01T00:00:00Z",
    });
  });

  it("leaves the task unchanged when the edit is cancelled", () => {
    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit task" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Discarded" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("shows an error and stays in the form when saving fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          error: { code: "invalid_title", message: "title must be 1-255 characters" },
        }),
        { status: 400 },
      ),
    );

    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit task" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("title must be 1-255 characters")).toBeInTheDocument();
    expect(screen.getByRole("form", { name: "Edit task" })).toBeInTheDocument();
  });

  it("rejects an empty title without calling the API", () => {
    render(<TaskDetail task={makeTask({})} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit task" }));
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByText("Task title is required.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("shows the start date alongside the due date", () => {
    render(
      <TaskDetail
        task={makeTask({ startDate: "2026-01-15" })}
        backlogs={[backlog]}
        tasks={[]}
        dependencies={[]}
      />,
    );
    expect(screen.getByText("Start")).toBeInTheDocument();
    expect(screen.getByText("Jan 15, 2026")).toBeInTheDocument();
  });

  it("shows Local only when the task has never been linked to GitLab", () => {
    render(<TaskDetail task={makeTask({ gitlab: null })} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    expect(screen.getByText("Local only")).toBeInTheDocument();
  });

  it("shows a link to the GitLab issue once synced", () => {
    const task = makeTask({
      gitlab: {
        syncStatus: "synced",
        lastError: "",
        lastSyncedAt: "2026-01-05T00:00:00Z",
        issueIid: 42,
        webUrl: "https://gitlab.example.com/group/demo/-/issues/42",
      },
    });
    render(<TaskDetail task={task} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    expect(screen.getByText("Synced")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View issue #42" })).toHaveAttribute(
      "href",
      "https://gitlab.example.com/group/demo/-/issues/42",
    );
  });

  it("shows the sync error and a retry button when sync failed", () => {
    const task = makeTask({
      gitlab: {
        syncStatus: "failed",
        lastError: "gitlab rejected the update",
        lastSyncedAt: null,
        issueIid: null,
        webUrl: "",
      },
    });
    render(<TaskDetail task={task} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    expect(screen.getByText("Sync failed")).toBeInTheDocument();
    expect(screen.getByText("gitlab rejected the update")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("retries a failed sync", async () => {
    const retried = makeTask({
      gitlab: {
        syncStatus: "pending",
        lastError: "",
        lastSyncedAt: null,
        issueIid: null,
        webUrl: "",
      },
    });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(retried), { status: 200 }));

    const task = makeTask({
      gitlab: {
        syncStatus: "failed",
        lastError: "gitlab rejected the update",
        lastSyncedAt: null,
        issueIid: null,
        webUrl: "",
      },
    });
    render(<TaskDetail task={task} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(screen.getByText("Syncing…")).toBeInTheDocument());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1/sync-retry",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows an error when retry fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "sync_not_failed", message: "gitlab sync is not currently failed" } }), {
        status: 409,
      }),
    );

    const task = makeTask({
      gitlab: {
        syncStatus: "failed",
        lastError: "gitlab rejected the update",
        lastSyncedAt: null,
        issueIid: null,
        webUrl: "",
      },
    });
    render(<TaskDetail task={task} backlogs={[backlog]} tasks={[]} dependencies={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(await screen.findByText("gitlab sync is not currently failed")).toBeInTheDocument();
  });
});
