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
  position: 0,
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
    position: 0,
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

/** The List mode is where the rows, their controls and reordering live; Board
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

  it("reorders by the move buttons, optimistically", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) });
    vi.stubGlobal("fetch", fetchMock);
    renderList({
      epics: [makeEpic(), makeEpic({ id: "e2", name: "API endpoints", position: 1 })],
    });

    fireEvent.click(screen.getByLabelText("Move API endpoints up"));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1/epics/order");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body as string)).toEqual({ epicIds: ["e2", "e1"] });

    // The optimistic order is on screen before any refresh.
    const names = screen
      .getAllByRole("link", { name: /Screens|API endpoints/ })
      .map((el) => el.textContent);
    expect(names).toEqual(["API endpoints", "Screens"]);
  });

  it("hides reordering whenever the list isn't the project's full manual order", () => {
    // A reorder request has to carry every epic in the project, so a narrowed
    // list would be certain to fail with an ID mismatch.
    renderList({ priorityFilter: "high" });
    expect(screen.queryByLabelText("Move Screens up")).not.toBeInTheDocument();
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

  it("edits an epic in place, sending its position so the order survives", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => makeEpic() });
    vi.stubGlobal("fetch", fetchMock);
    renderList({ epics: [makeEpic({ position: 3 })] });

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
      position: 3,
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
});
