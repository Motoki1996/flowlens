import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within, act } from "@testing-library/react";
import type { Backlog, Task } from "@/types";
import { TaskListSection } from "./TaskListSection";

const refresh = vi.fn();
const push = vi.fn();
// The screen's filters are the URL (issue #143): every filter control pushes a
// query string for the server component to re-fetch with, so what these tests
// assert about filtering is the query pushed, not a list narrowed in place.
let currentSearchParams = new URLSearchParams();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh, push }),
  usePathname: () => "/projects/p1/tasks",
  useSearchParams: () => currentSearchParams,
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
    priority: "medium",
    progress: "not_started",
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
  priority: "medium",
  progress: "not_started",
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const otherBacklog: Backlog = {
  ...backlog,
  id: "b2",
  name: "Icebox",
  position: 1,
};

/** The screen opens in Board view, so the List-view specifics — backlog
 *  grouping, selection checkboxes, manual reordering — need the List toggle
 *  clicked first. */
function showListView() {
  fireEvent.click(screen.getByRole("button", { name: "List" }));
}

/** The inline creation form, for the fields whose labels it shares with the
 *  collection's filter row (both name a "Backlog"). */
function withinNewTaskForm() {
  return within(screen.getByRole("form", { name: "New task" }));
}

describe("TaskListSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    push.mockClear();
    currentSearchParams = new URLSearchParams();
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("reports an empty result in terms of the filter that produced it", () => {
    // With the filtering server-side there is no second, unfiltered list to
    // tell "this project has no tasks" from "nothing matched" — and the
    // status filter, defaulting to Open, is always one of the reasons.
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
    expect(screen.getByText("No open tasks.")).toBeInTheDocument();
  });

  it("keeps the filters reachable when the result is empty, so the filter can be undone", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} search="nothing" />);
    expect(screen.getByRole("combobox", { name: "Status" })).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Search tasks" })).toHaveValue("nothing");
    expect(screen.getByText('No tasks match "nothing".')).toBeInTheDocument();
  });

  it("groups tasks with no backlog under Unclassified", () => {
    const tasks = [makeTask({ id: "t1", title: "Unfiled task", backlogId: null })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
    showListView();
    expect(screen.getByText("Unclassified (1)")).toBeInTheDocument();
    expect(screen.getByText("Unfiled task")).toBeInTheDocument();
  });

  it("groups tasks by backlog, ordered before the Unclassified group", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Filed task", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Unfiled task", backlogId: null }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
    showListView();
    const headings = screen.getAllByRole("heading", { level: 3 }).map((h) => h.textContent);
    expect(headings).toEqual(["Sprint 1 (1)", "Unclassified (1)"]);
  });

  it("renders the tasks the API returned, without filtering them again itself", () => {
    // The status filter defaults to Open, but what is rendered is whatever
    // came back: the API is the only thing that decides membership now, so a
    // list that disagrees with the controls is still shown as-is rather than
    // being quietly narrowed a second time.
    const tasks = [
      makeTask({ id: "t1", title: "Open task", status: "open" }),
      makeTask({ id: "t2", title: "Closed task", status: "closed" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);

    expect(screen.getByText("Open task")).toBeInTheDocument();
    expect(screen.getByText("Closed task")).toBeInTheDocument();
  });

  it("pushes ?status= when the status filter is widened", async () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);

    fireEvent.click(screen.getByRole("combobox", { name: "Status" }));
    fireEvent.click(await screen.findByRole("option", { name: "All statuses" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all");
  });

  it("drops a filter back out of the query string when it returns to its default", async () => {
    currentSearchParams = new URLSearchParams("status=all");
    const { unmount } = render(
      <TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />,
    );

    fireEvent.click(screen.getByRole("combobox", { name: "Status" }));
    fireEvent.click(await screen.findByRole("option", { name: "Open" }));
    expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
    unmount();

    // …and the manual order is the same kind of default: it is the absence of
    // ?sort=, not a value of its own.
    currentSearchParams = new URLSearchParams("sort=priority");
    push.mockClear();
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} sort="priority" />);
    fireEvent.click(screen.getByRole("combobox", { name: "Sort" }));
    fireEvent.click(await screen.findByRole("option", { name: "Manual order" }));
    expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
  });

  it("pushes the search text as ?q= once the debounce elapses, matched server-side", () => {
    vi.useFakeTimers();
    // Unlike the old client-side substring match, `?q=` goes to the API's
    // websearch_to_tsquery: it matches whole tokenized words in the title or
    // description, so "logi" no longer finds "login". Nothing is filtered
    // here — the next server render is what narrows the list.
    const tasks = [makeTask({ id: "t1", title: "Fix login bug" })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);

    fireEvent.change(screen.getByRole("textbox", { name: "Search tasks" }), {
      target: { value: "login" },
    });
    expect(push).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(300);
    });

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?q=login");
  });

  it("pushes ?sort= and renders in the order the API returned", () => {
    // The API applies the sort, so the rendered order is the response order —
    // these tasks are deliberately not in due-date order to prove the screen
    // doesn't re-sort them.
    const tasks = [
      makeTask({ id: "t1", title: "Urgent, due later", priority: "urgent", dueOn: "2026-02-01" }),
      makeTask({ id: "t2", title: "Medium, due sooner", priority: "medium", dueOn: "2026-01-15" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} sort="priority" />);
    showListView();

    const titles = screen.getAllByRole("link").map((el) => el.querySelector("span")?.textContent);
    expect(titles).toEqual(["Urgent, due later", "Medium, due sooner"]);

    fireEvent.click(screen.getByRole("combobox", { name: "Sort" }));
    fireEvent.click(screen.getByRole("option", { name: "Due date" }));
    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?sort=dueOn");
  });

  it("pushes ?progress= alongside the other filters rather than replacing them", async () => {
    currentSearchParams = new URLSearchParams("status=all");
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />);

    fireEvent.click(screen.getByRole("combobox", { name: "Progress" }));
    fireEvent.click(await screen.findByRole("option", { name: "On hold" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all&progress=on_hold");
  });

  it("shows the applied filters from the URL query the screen was opened with", () => {
    render(
      <TaskListSection
        projectId="p1"
        tasks={[makeTask({ id: "t1", title: "Held task", progress: "on_hold" })]}
        backlogs={[]}
        search="urgent"
        statusFilter="all"
        sort="priority"
        progressFilter="on_hold"
        priorityFilter="high"
      />,
    );

    expect(screen.getByRole("textbox", { name: "Search tasks" })).toHaveValue("urgent");
    expect(screen.getByRole("combobox", { name: "Status" })).toHaveTextContent("All statuses");
    expect(screen.getByRole("combobox", { name: "Progress" })).toHaveTextContent("On hold");
    expect(screen.getByRole("combobox", { name: "Priority" })).toHaveTextContent("High");
    expect(screen.getByRole("combobox", { name: "Sort" })).toHaveTextContent("Priority");
  });

  it("pushes ?priority= alongside the other filters rather than replacing them", async () => {
    currentSearchParams = new URLSearchParams("status=all");
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />);

    fireEvent.click(screen.getByRole("combobox", { name: "Priority" }));
    fireEvent.click(await screen.findByRole("option", { name: "Urgent" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all&priority=urgent");
  });

  it("drops ?priority= back out of the query string when it returns to All priorities", async () => {
    currentSearchParams = new URLSearchParams("priority=urgent");
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} priorityFilter="urgent" />);

    fireEvent.click(screen.getByRole("combobox", { name: "Priority" }));
    fireEvent.click(await screen.findByRole("option", { name: "All priorities" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
  });

  it("reports an empty result from the priority filter", () => {
    render(
      <TaskListSection
        projectId="p1"
        tasks={[]}
        backlogs={[]}
        statusFilter="all"
        priorityFilter="urgent"
      />,
    );
    expect(screen.getByText("No urgent priority tasks.")).toBeInTheDocument();
  });

  it("shows a load error", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} error />);
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });

  describe("My tasks filter (issue #146)", () => {
    it("pushes ?assignee=me alongside the other filters when checked", () => {
      currentSearchParams = new URLSearchParams("status=all");
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />);

      fireEvent.click(screen.getByRole("checkbox", { name: "My tasks" }));
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all&assignee=me");
    });

    it("drops ?assignee=me back out of the query string when unchecked", () => {
      currentSearchParams = new URLSearchParams("status=all&assignee=me");
      render(
        <TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" assigneeMe />,
      );

      fireEvent.click(screen.getByRole("checkbox", { name: "My tasks" }));
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all");
    });

    it("reflects ?assignee=me from the URL as checked", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} assigneeMe />);
      expect(screen.getByRole("checkbox", { name: "My tasks" })).toBeChecked();
    });

    it("reports an empty result from the My tasks filter", () => {
      render(
        <TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" assigneeMe />,
      );
      expect(screen.getByText("No tasks are assigned to you.")).toBeInTheDocument();
    });

    it("disables the checkbox and links to the GitLab connection when the project has none", () => {
      render(
        <TaskListSection
          projectId="p1"
          tasks={[]}
          backlogs={[]}
          assigneeAvailability="no-connection"
        />,
      );
      expect(screen.getByRole("checkbox", { name: "My tasks" })).toBeDisabled();
      expect(screen.getByRole("link", { name: "Connect GitLab" })).toHaveAttribute(
        "href",
        "/projects/p1/gitlab-connection",
      );
    });

    it("disables the checkbox and links to Settings when the caller has no matching identity", () => {
      render(
        <TaskListSection projectId="p1" tasks={[]} backlogs={[]} assigneeAvailability="no-identity" />,
      );
      expect(screen.getByRole("checkbox", { name: "My tasks" })).toBeDisabled();
      expect(screen.getByRole("link", { name: "Register GitLab identity" })).toHaveAttribute(
        "href",
        "/settings",
      );
    });

    it("leaves the checkbox enabled with no reason link once a matching identity is registered", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
      expect(screen.getByRole("checkbox", { name: "My tasks" })).not.toBeDisabled();
      expect(screen.queryByRole("link", { name: "Connect GitLab" })).not.toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Register GitLab identity" })).not.toBeInTheDocument();
    });
  });

  it("assigns selected unclassified tasks to a backlog in bulk", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    const tasks = [
      makeTask({ id: "t1", title: "Unfiled task 1", backlogId: null }),
      makeTask({ id: "t2", title: "Unfiled task 2", backlogId: null }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);

    showListView();
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

  it("pushes ?backlog= for the backlog picked in the filter", async () => {
    const tasks = [
      makeTask({ id: "t1", title: "Sprint task", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Icebox task", backlogId: "b2" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);

    fireEvent.click(screen.getByRole("combobox", { name: "Backlog" }));
    fireEvent.click(await screen.findByRole("option", { name: "Icebox" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?backlog=b2");
  });

  it("offers selection for tasks already in a backlog, so they can move to another backlog or back to Unclassified", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    const tasks = [makeTask({ id: "t1", title: "Filed task", backlogId: "b1" })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);

    showListView();
    fireEvent.click(screen.getByRole("checkbox", { name: "Select Filed task" }));
    fireEvent.click(screen.getByRole("combobox", { name: "Backlog to assign" }));
    fireEvent.click(await screen.findByRole("option", { name: "Unclassified" }));
    fireEvent.click(screen.getByRole("button", { name: "Assign to backlog" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/tasks/t1/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: null }) }),
    );
  });

  it("does not offer selection when the project has no backlogs to move tasks into", () => {
    // Scoped to the row-selection checkboxes ("Select <task>") rather than
    // any checkbox: the "My tasks" filter checkbox (issue #146) is always in
    // the filter row regardless of backlogs.
    const tasks = [makeTask({ id: "t1", title: "Only task", backlogId: null })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
    showListView();
    expect(screen.queryByRole("checkbox", { name: "Select Only task" })).not.toBeInTheDocument();
  });

  it("opens in the board view mode, showing the tasks the API returned by progress", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Fix login", progress: "in_progress" }),
      makeTask({ id: "t2", title: "Done bug", progress: "done", status: "closed" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} statusFilter="all" />);

    const inProgress = screen.getByRole("region", { name: "In progress tasks" });
    expect(within(inProgress).getByRole("link", { name: "Fix login" })).toBeInTheDocument();
    const done = screen.getByRole("region", { name: "Done tasks" });
    expect(within(done).getByRole("link", { name: "Done bug" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Status" })).toBeInTheDocument();
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

  it("shows the backlog-filtered set the API returned in the timeline too", async () => {
    // The API applied ?backlog=b2, so the timeline gets exactly the same
    // narrowed set the list and board do — the three modes never disagree.
    const tasks = [makeTask({ id: "t2", title: "Icebox task", backlogId: "b2", startDate: "2026-08-02" })];
    render(
      <TaskListSection
        projectId="p1"
        tasks={tasks}
        backlogs={[backlog, otherBacklog]}
        backlogFilter="b2"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    expect(
      await screen.findByRole("link", { name: "Icebox task" }, { timeout: 15000 }),
    ).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Icebox");
  });

  it("reports an empty filter result the same way in either view mode", () => {
    render(
      <TaskListSection
        projectId="p1"
        tasks={[]}
        backlogs={[backlog, otherBacklog]}
        backlogFilter="b2"
      />,
    );

    expect(screen.getByText("No open tasks in Icebox.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    expect(screen.getByText("No open tasks in Icebox.")).toBeInTheDocument();
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
          priority: "medium",
          progress: "not_started",
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
    // Scoped to the form: the collection's own backlog *filter* carries the
    // same name, and both are on screen at once.
    fireEvent.click(withinNewTaskForm().getByLabelText("Backlog"));
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
    fireEvent.click(withinNewTaskForm().getByLabelText("Backlog"));
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

  it("moves a task within its backlog with the move-down button, updating the display order optimistically", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    const tasks = [
      makeTask({ id: "t1", title: "First", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Second", backlogId: "b1" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);

    showListView();
    fireEvent.click(screen.getByRole("button", { name: "Move First down" }));

    // The row order updates immediately, ahead of the API round trip.
    const titles = screen.getAllByText(/^(First|Second)$/).map((el) => el.textContent);
    expect(titles).toEqual(["Second", "First"]);
    expect(fetch).toHaveBeenCalledWith(
      "http://localhost:8080/api/v1/projects/p1/tasks/order",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({ backlogId: "b1", taskIds: ["t2", "t1"] }),
      }),
    );
  });

  it("reverts the order and shows an error when the reorder request fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "task_ids_mismatch", message: "taskIds must match" } }), {
        status: 400,
      }),
    );
    const tasks = [
      makeTask({ id: "t1", title: "First", backlogId: "b1" }),
      makeTask({ id: "t2", title: "Second", backlogId: "b1" }),
    ];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);

    showListView();
    fireEvent.click(screen.getByRole("button", { name: "Move First down" }));

    expect(await screen.findByText("taskIds must match")).toBeInTheDocument();
    const titles = screen.getAllByText(/^(First|Second)$/).map((el) => el.textContent);
    expect(titles).toEqual(["First", "Second"]);
  });

  it("hides the drag handle and move buttons while sorted by anything other than the manual order", () => {
    const tasks = [makeTask({ id: "t1", title: "Only task", backlogId: "b1" })];
    const { unmount } = render(
      <TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />,
    );
    showListView();
    expect(screen.getByRole("button", { name: "Move Only task down" })).toBeInTheDocument();
    unmount();

    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} sort="priority" />);
    showListView();
    expect(screen.queryByRole("button", { name: "Move Only task down" })).not.toBeInTheDocument();
  });
});
