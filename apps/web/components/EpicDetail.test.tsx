import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { Backlog, Epic, LinkedGitlabProject, Task } from "@/types";
import { EpicDetail } from "./EpicDetail";

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
  baseBranch: "main",
  allowedScope: "apps/**",
  forbiddenScope: "migrations/**",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function makeEpic(overrides: Partial<Epic> = {}): Epic {
  return {
    id: "e1",
    projectId: "p1",
    backlogId: "b1",
    name: "Screens",
    description: "",
    startDate: null,
    dueOn: null,
    priority: "medium",
    progress: "not_started",
    defaultLinkedGitlabProjectId: null,
    baseBranch: "",
    allowedScope: "",
    forbiddenScope: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    taskCount: 0,
    closedTaskCount: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const project = { id: "p1", name: "Alpha" };

beforeEach(() => {
  refresh.mockClear();
  push.mockClear();
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("EpicDetail", () => {
  // The inheritance is the one thing this screen has that BacklogDetail
  // doesn't: only a value the epic sets itself follows the epic, so the
  // reader has to be able to tell the two apart.
  it("marks an inherited base branch and scope as coming from the backlog", () => {
    render(<EpicDetail epic={makeEpic()} project={project} backlog={backlog} />);

    expect(screen.getByText("main")).toBeInTheDocument();
    expect(screen.getAllByText(/\(from backlog\)/).length).toBe(3);
  });

  it("shows the epic's own value without the inherited marker", () => {
    render(
      <EpicDetail
        epic={makeEpic({ baseBranch: "release/2.4" })}
        project={project}
        backlog={backlog}
      />,
    );

    expect(screen.getByText("release/2.4")).toBeInTheDocument();
    // Scope is still inherited; the chain runs per field, not per object.
    expect(screen.getAllByText(/\(from backlog\)/).length).toBe(2);
  });

  it("reads 'Not set' when neither the epic nor its backlog sets one", () => {
    render(<EpicDetail epic={makeEpic({ backlogId: null })} project={project} backlog={null} />);
    expect(screen.getAllByText("Not set").length).toBeGreaterThan(0);
    expect(screen.getByText("No backlog")).toBeInTheDocument();
  });

  it("names the issue destination the epic's tasks will actually use", () => {
    const links: LinkedGitlabProject[] = [
      {
        id: "l1",
        gitlabConnectionId: "c1",
        gitlabProjectId: 100,
        pathWithNamespace: "group/demo",
        name: "demo",
        webUrl: "https://gitlab.example.com/group/demo",
        syncScope: "all",
        syncLabels: [],
        isDefault: true,
        initialImportStatus: "completed",
        lastSyncedAt: null,
        webhookStatus: "registered",
        webhookRegisteredAt: null,
        webhookError: "",
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      },
    ];
    render(
      <EpicDetail epic={makeEpic()} project={project} backlog={backlog} links={links} />,
    );

    expect(screen.getByText(/group\/demo/)).toBeInTheDocument();
    expect(screen.getByText(/\(project default\)/)).toBeInTheDocument();
  });

  it("links its tasks to the Task collection rather than listing them twice", () => {
    const task: Task = {
      id: "t1",
      projectId: "p1",
      backlogId: "b1",
      epicId: "e1",
      title: "Build the list screen",
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
    render(<EpicDetail epic={makeEpic()} project={project} backlog={backlog} tasks={[task]} />);

    expect(screen.getByRole("link", { name: "Open in Tasks" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks?epic=e1",
    );
    expect(screen.getByRole("link", { name: /Build the list screen/ })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t1",
    );
  });

  it("edits in place and reads back the saved values", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => makeEpic({ name: "Screens v2" }) });
    vi.stubGlobal("fetch", fetchMock);
    render(<EpicDetail epic={makeEpic()} project={project} backlog={backlog} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit epic" }));
    const form = within(screen.getByRole("form", { name: "Edit Screens" }));
    fireEvent.change(form.getByLabelText("Name"), { target: { value: "Screens v2" } });
    fireEvent.click(form.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.getByRole("heading", { name: "Screens v2" })).toBeInTheDocument());
    expect(refresh).toHaveBeenCalled();
  });

  it("hands back to the collection once the epic is deleted", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 });
    vi.stubGlobal("fetch", fetchMock);
    render(<EpicDetail epic={makeEpic()} project={project} backlog={backlog} />);

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/projects/p1/epics"));
  });

  // The epic's own half of the relationship. The task side is the Epic
  // control on the task's single view; both write the same link.
  describe("editing which tasks are in the epic", () => {
    const inEpic = {
      id: "t1",
      projectId: "p1",
      backlogId: "b1",
      epicId: "e1",
      title: "Build the list screen",
      description: "",
      status: "open" as const,
      closedAt: null,
      assigneeGitlabUserId: null,
      assigneeGitlabUsername: "",
      assigneeUserId: null,
      assigneeUsername: "",
      assigneeDisplayName: "",
      labels: [],
      dueOn: null,
      startDate: null,
      priority: "medium" as const,
      progress: "not_started" as const,
      size: "m" as const,
      designStartedAt: null,
      implementationStartedAt: null,
      createdByUserId: "u1",
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      gitlab: null,
      aiContext: { acceptanceCriteria: "", aiContext: "", updatedAt: null },
    };
    const free = { ...inEpic, id: "t2", title: "Build the detail screen", epicId: null };

    it("adds a free task from the epic's backlog, as a whole set", async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => makeEpic() });
      vi.stubGlobal("fetch", fetchMock);

      render(
        <EpicDetail
          epic={makeEpic()}
          project={project}
          backlog={backlog}
          tasks={[inEpic]}
          projectTasks={[inEpic, free]}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "Edit tasks" }));
      fireEvent.click(screen.getByRole("option", { name: /Build the detail screen/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save tasks" }));

      await waitFor(() => expect(fetchMock).toHaveBeenCalled());
      const [url, init] = fetchMock.mock.calls[0];
      expect(url).toBe("/api/v1/epics/e1/tasks");
      expect(init.method).toBe("PATCH");
      expect(JSON.parse(init.body as string)).toEqual({ taskIds: ["t1", "t2"] });
      await waitFor(() => expect(refresh).toHaveBeenCalled());
    });

    it("removes one by unticking it", async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => makeEpic() });
      vi.stubGlobal("fetch", fetchMock);

      render(
        <EpicDetail
          epic={makeEpic()}
          project={project}
          backlog={backlog}
          tasks={[inEpic]}
          projectTasks={[inEpic, free]}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "Edit tasks" }));
      fireEvent.click(screen.getByRole("option", { name: /Build the list screen/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save tasks" }));

      await waitFor(() => expect(fetchMock).toHaveBeenCalled());
      const [, init] = fetchMock.mock.calls[0];
      expect(JSON.parse(init.body as string)).toEqual({ taskIds: [] });
    });

    it("reports a failure without claiming the change stuck", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        json: async () => ({ error: { code: "invalid_tasks", message: "nope" } }),
      });
      vi.stubGlobal("fetch", fetchMock);

      render(
        <EpicDetail
          epic={makeEpic()}
          project={project}
          backlog={backlog}
          tasks={[inEpic]}
          projectTasks={[inEpic, free]}
        />,
      );

      fireEvent.click(screen.getByRole("button", { name: "Edit tasks" }));
      fireEvent.click(screen.getByRole("option", { name: /Build the detail screen/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save tasks" }));

      await waitFor(() => expect(screen.getByText("nope")).toBeInTheDocument());
      expect(refresh).not.toHaveBeenCalled();
    });
  });
});
