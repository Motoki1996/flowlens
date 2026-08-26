import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import type { Backlog, Epic } from "@/types";
import { EpicListSection } from "./EpicListSection";

const refresh = vi.fn();
const push = vi.fn();
// The filters are the URL, the same as the Backlog collection's, so what
// these tests assert about filtering is the query pushed and, for the
// client-only `sort=dueOn`/`?q=`, the list actually rendered.
let currentSearchParams = new URLSearchParams();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh, push }),
  usePathname: () => "/projects/p1/epics",
  useSearchParams: () => currentSearchParams,
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
  allowedScope: "",
  forbiddenScope: "",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  taskCount: 0,
  closedTaskCount: 0,
  status: "open" as const,
  closedAt: null,
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
    estimatedPoints: null,
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    taskCount: 0,
    closedTaskCount: 0,
    status: "open" as const,
    closedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

/** The List mode is where the rows and their controls live; Board
 *  is the default, so every list assertion below opens List first. */
function renderList(props: Partial<Parameters<typeof EpicListSection>[0]> = {}) {
  return render(
    <EpicListSection
      projectId="p1"
      epics={[makeEpic()]}
      backlogs={[backlog]}
      initialView="list"
      {...props}
    />,
  );
}

beforeEach(() => {
  currentSearchParams = new URLSearchParams();
  refresh.mockClear();
  push.mockClear();
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("EpicListSection", () => {
  it("lists each epic with its backlog, ratio and links", () => {
    renderList({
      epics: [makeEpic({ taskCount: 4, closedTaskCount: 1, baseBranch: "release/2.4" })],
    });

    expect(screen.getByRole("link", { name: "Screens" })).toHaveAttribute(
      "href",
      "/projects/p1/epics/e1",
    );
    expect(screen.getByText(/Sprint 1/)).toBeInTheDocument();
    expect(screen.getByText(/release\/2.4/)).toBeInTheDocument();
    expect(screen.getByText("1/4 closed")).toBeInTheDocument();
    // Tasks live in the Task collection, filtered — the row hands off rather
    // than growing a second place to browse tasks.
    expect(screen.getByRole("link", { name: "View tasks" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks?epic=e1",
    );
  });

  it("says so when an epic is in no backlog, rather than hiding it", () => {
    renderList({ epics: [makeEpic({ backlogId: null })] });
    expect(screen.getByText(/No backlog/)).toBeInTheDocument();
  });

  it("shows the empty state, and keeps New epic reachable", () => {
    renderList({ epics: [] });
    expect(screen.getByText("No epics yet.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /New epic/ })).toBeInTheDocument();
  });

  it("reports a load failure instead of an empty collection", () => {
    renderList({ epics: [], error: true });
    expect(screen.getByText(/Failed to load epics/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Priority")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /New epic/ })).toBeInTheDocument();
  });

  it("puts each filter in the URL", () => {
    renderList();

    fireEvent.click(screen.getByLabelText("Backlog"));
    fireEvent.click(screen.getByRole("option", { name: "Sprint 1" }));
    expect(push).toHaveBeenCalledWith("/projects/p1/epics?backlog=b1");
  });

  it("drops a filter from the URL when it returns to its default", () => {
    currentSearchParams = new URLSearchParams("priority=high");
    renderList({ priorityFilter: "high" });

    fireEvent.click(screen.getByLabelText("Priority"));
    fireEvent.click(screen.getByRole("option", { name: "All priorities" }));
    expect(push).toHaveBeenCalledWith("/projects/p1/epics");
  });

  it("says why the list is empty when a filter is what emptied it", () => {
    renderList({ epics: [], priorityFilter: "urgent" });
    expect(screen.getByText("No urgent priority epics.")).toBeInTheDocument();
  });

  it("matches ?q= against the name, client-side", () => {
    currentSearchParams = new URLSearchParams("q=api");
    renderList({
      epics: [makeEpic(), makeEpic({ id: "e2", name: "API endpoints" })],
    });

    expect(screen.getByRole("link", { name: "API endpoints" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Screens" })).not.toBeInTheDocument();
  });

  it("creates an epic through the collection's own form", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => makeEpic({ id: "e9" }) });
    vi.stubGlobal("fetch", fetchMock);
    renderList({ epics: [] });

    fireEvent.click(screen.getByRole("button", { name: /New epic/ }));
    const form = within(screen.getByRole("form", { name: "New epic" }));
    fireEvent.change(form.getByLabelText("Name"), { target: { value: "Screens" } });
    fireEvent.click(form.getByRole("button", { name: "Create epic" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1/epics");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toMatchObject({ name: "Screens", backlogId: null });
    await waitFor(() => expect(refresh).toHaveBeenCalled());
  });

  it("pre-selects the filtered backlog on a new epic", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => makeEpic() });
    vi.stubGlobal("fetch", fetchMock);
    renderList({ epics: [], backlogFilter: "b1" });

    fireEvent.click(screen.getByRole("button", { name: /New epic/ }));
    const form = within(screen.getByRole("form", { name: "New epic" }));
    fireEvent.change(form.getByLabelText("Name"), { target: { value: "Screens" } });
    fireEvent.click(form.getByRole("button", { name: "Create epic" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body as string)).toMatchObject({ backlogId: "b1" });
  });

  it("edits an epic in place", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => makeEpic() });
    vi.stubGlobal("fetch", fetchMock);
    renderList({ epics: [makeEpic()] });

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const form = within(screen.getByRole("form", { name: "Edit Screens" }));
    fireEvent.change(form.getByLabelText("Name"), { target: { value: "Screens v2" } });
    fireEvent.click(form.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/epics/e1");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body as string)).toMatchObject({
      name: "Screens v2",
    });
  });

  it("spells out that a delete keeps the epic's tasks", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 });
    vi.stubGlobal("fetch", fetchMock);
    renderList();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(screen.getByText(/tasks will stay in their backlog/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/epics/e1");
    expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
  });

  // The epic side of the task<->epic relationship, offered on create as well
  // as edit: a coarse unit is usually cut out of tasks that already exist.
  it("files existing tasks into a new epic in the same save", async () => {
    const task = {
      id: "t1",
      projectId: "p1",
      backlogId: "b1",
      epicId: null,
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
    const fetchMock = vi
      .fn()
      .mockResolvedValue({ ok: true, json: async () => makeEpic({ id: "e9" }) });
    vi.stubGlobal("fetch", fetchMock);

    renderList({ epics: [], tasks: [task], backlogFilter: "b1" });

    fireEvent.click(screen.getByRole("button", { name: /New epic/ }));
    const form = within(screen.getByRole("form", { name: "New epic" }));
    fireEvent.change(form.getByLabelText("Name"), { target: { value: "Screens" } });
    fireEvent.click(form.getByRole("option", { name: /Build the list screen/ }));
    fireEvent.click(form.getByRole("button", { name: "Create epic" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    // The epic first, then its task set — a relationship between two objects,
    // not a column on either.
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/projects/p1/epics");
    expect(fetchMock.mock.calls[1][0]).toBe("/api/v1/epics/e9/tasks");
    expect(fetchMock.mock.calls[1][1].method).toBe("PATCH");
    expect(JSON.parse(fetchMock.mock.calls[1][1].body as string)).toEqual({ taskIds: ["t1"] });
  });

  it("drops the task picker when the caller has no task list to offer", () => {
    renderList({ epics: [] });
    fireEvent.click(screen.getByRole("button", { name: /New epic/ }));
    expect(screen.queryByLabelText("Tasks in this epic")).not.toBeInTheDocument();
  });
});

// --- Status filter (000036) --------------------------------------------------

describe("the status filter", () => {
  it("defaults to Open", () => {
    renderList();
    expect(screen.getByRole("combobox", { name: "Status" })).toHaveTextContent("Open");
  });

  it("pushes ?status= only when it leaves the open-only default", async () => {
    renderList();

    fireEvent.click(screen.getByRole("combobox", { name: "Status" }));
    fireEvent.click(await screen.findByRole("option", { name: "All statuses" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/epics?status=all");
  });

  it("offers a way back to the closed epics when the open list is empty", async () => {
    renderList({ epics: [] });

    fireEvent.click(screen.getByRole("button", { name: "Show closed epics" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/epics?status=all");
  });

  it("marks a closed epic in the list, and never marks an open one", () => {
    renderList({
      epics: [makeEpic(), makeEpic({ id: "e2", name: "Endpoints", status: "closed" })],
      statusFilter: "all",
    });

    expect(screen.getAllByText("Closed")).toHaveLength(1);
  });

  /** The Epic collection's own half of the bulk editing the Task and Backlog
   *  collections have — the four shared actions plus "Move to backlog",
   *  which carries each epic's tasks along with it. */
  describe("Bulk actions", () => {
    const second = makeEpic({ id: "e2", name: "API" });

    it("shows no selection bar until something is selected", () => {
      renderList();
      expect(screen.queryByText(/selected/)).not.toBeInTheDocument();
    });

    it("selects every visible epic at once", () => {
      renderList({ epics: [makeEpic(), second] });
      fireEvent.click(
        screen.getByRole("checkbox", { name: "Select all epics" }),
      );
      expect(screen.getByText("2 selected")).toBeInTheDocument();
    });

    it("applies a bulk priority change to every selected epic", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      renderList({ epics: [makeEpic(), second] });
      fireEvent.click(
        screen.getByRole("checkbox", { name: "Select all epics" }),
      );

      fireEvent.click(
        screen.getByRole("combobox", { name: "Priority to set" }),
      );
      fireEvent.click(await screen.findByRole("option", { name: "Urgent" }));
      fireEvent.click(screen.getByRole("button", { name: "Set priority" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      for (const id of ["e1", "e2"]) {
        expect(fetch).toHaveBeenCalledWith(
          `/api/v1/epics/${id}`,
          expect.objectContaining({
            method: "PATCH",
            body: JSON.stringify({ priority: "urgent" }),
          }),
        );
      }
      expect(screen.queryByText(/selected/)).not.toBeInTheDocument();
    });

    it("applies a bulk progress change to every selected epic", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      renderList();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Screens" }));

      fireEvent.click(
        screen.getByRole("combobox", { name: "Progress to set" }),
      );
      fireEvent.click(await screen.findByRole("option", { name: "On hold" }));
      fireEvent.click(screen.getByRole("button", { name: "Set progress" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/epics/e1",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ progress: "on_hold" }),
        }),
      );
    });

    it("moves every selected epic into the chosen backlog", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      renderList({ epics: [makeEpic({ backlogId: null })] });
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Screens" }));

      fireEvent.click(
        screen.getByRole("combobox", { name: "Backlog to move to" }),
      );
      fireEvent.click(await screen.findByRole("option", { name: "Sprint 1" }));
      fireEvent.click(screen.getByRole("button", { name: "Move to backlog" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/epics/e1",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ backlogId: "b1" }),
        }),
      );
    });

    it("moves a selected epic out of every backlog", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      renderList();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Screens" }));

      fireEvent.click(
        screen.getByRole("combobox", { name: "Backlog to move to" }),
      );
      fireEvent.click(
        await screen.findByRole("option", { name: "No backlog" }),
      );
      fireEvent.click(screen.getByRole("button", { name: "Move to backlog" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/epics/e1",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ backlogId: null }),
        }),
      );
    });

    it("closes and reopens every selected epic in bulk", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      renderList();

      fireEvent.click(screen.getByRole("checkbox", { name: "Select Screens" }));
      fireEvent.click(screen.getByRole("button", { name: "Close selected" }));
      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/epics/e1/close",
        expect.objectContaining({ method: "POST" }),
      );

      fireEvent.click(screen.getByRole("checkbox", { name: "Select Screens" }));
      fireEvent.click(screen.getByRole("button", { name: "Reopen selected" }));
      await waitFor(() =>
        expect(fetch).toHaveBeenCalledWith(
          "/api/v1/epics/e1/reopen",
          expect.objectContaining({ method: "POST" }),
        ),
      );
    });

    it("reports a wholesale failure and keeps the selection", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 500 }));
      renderList();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Screens" }));
      fireEvent.click(screen.getByRole("button", { name: "Close selected" }));

      await waitFor(() =>
        expect(
          screen.getByText("Failed to update 1 epic."),
        ).toBeInTheDocument(),
      );
      expect(screen.getByText("1 selected")).toBeInTheDocument();
    });
  });
});
