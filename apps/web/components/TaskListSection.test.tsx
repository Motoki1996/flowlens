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
    size: "m",
    designStartedAt: null,
    implementationStartedAt: null,
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
  defaultLinkedGitlabProjectId: null,
  baseBranch: "",
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

  it("pushes ?size= alongside the other filters rather than replacing them", async () => {
    currentSearchParams = new URLSearchParams("status=all");
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />);

    fireEvent.click(screen.getByRole("combobox", { name: "Size" }));
    fireEvent.click(await screen.findByRole("option", { name: "XL" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all&size=xl");
  });

  it("drops ?size= back out of the query string when it returns to All sizes", async () => {
    currentSearchParams = new URLSearchParams("size=xl");
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} sizeFilter="xl" />);

    fireEvent.click(screen.getByRole("combobox", { name: "Size" }));
    fireEvent.click(await screen.findByRole("option", { name: "All sizes" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
  });

  it("reports an empty result from the size filter", () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" sizeFilter="xs" />);
    expect(screen.getByText("No XS tasks.")).toBeInTheDocument();
  });

  it("pushes ?sort=size, leaving the ordering to the API", async () => {
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);

    fireEvent.click(screen.getByRole("combobox", { name: "Sort" }));
    fireEvent.click(await screen.findByRole("option", { name: "Size" }));

    expect(push).toHaveBeenCalledWith("/projects/p1/tasks?sort=size");
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

  describe("Result count and Clear filters (issue #150)", () => {
    it("shows the filtered count against the project total", () => {
      const tasks = [makeTask({ id: "t1" }), makeTask({ id: "t2" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} totalCount={5} />);
      expect(screen.getByText("2 / 5 tasks")).toBeInTheDocument();
    });

    it("shows just the filtered count when no total was passed", () => {
      const tasks = [makeTask({ id: "t1" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      expect(screen.getByText("1 tasks")).toBeInTheDocument();
    });

    it("omits the count on a load error", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} totalCount={5} error />);
      expect(screen.queryByText(/tasks$/)).not.toBeInTheDocument();
    });

    it("hides Clear filters when every filter is at its default", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
      expect(screen.queryByRole("button", { name: "Clear filters" })).not.toBeInTheDocument();
    });

    it("shows Clear filters once a filter differs from its default, and resets the URL on click", () => {
      currentSearchParams = new URLSearchParams("status=all");
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />);

      const clearButton = screen.getByRole("button", { name: "Clear filters" });
      fireEvent.click(clearButton);
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
    });

    it("shows Clear filters for a non-default sort, same as any other filter", () => {
      currentSearchParams = new URLSearchParams("sort=priority");
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} sort="priority" />);
      expect(screen.getByRole("button", { name: "Clear filters" })).toBeInTheDocument();
    });
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

  describe("Label filter (issue #147)", () => {
    it("pushes ?label= when a label badge is clicked, without navigating to the task", () => {
      const tasks = [makeTask({ id: "t1", title: "Buggy task", labels: ["bug"] })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();

      fireEvent.click(screen.getByRole("button", { name: "bug" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?label=bug");
      expect(push).not.toHaveBeenCalledWith("/projects/p1/tasks/t1");
    });

    it("clears the label filter when the same badge is clicked again", () => {
      currentSearchParams = new URLSearchParams("label=bug");
      const tasks = [makeTask({ id: "t1", title: "Buggy task", labels: ["bug"] })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();

      fireEvent.click(screen.getByRole("button", { name: "bug" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
    });

    it("shows only the tasks carrying the active label, alongside a clear chip", () => {
      currentSearchParams = new URLSearchParams("label=bug");
      const tasks = [
        makeTask({ id: "t1", title: "Buggy task", labels: ["bug"] }),
        makeTask({ id: "t2", title: "Feature task", labels: ["feature"] }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();

      expect(screen.getByText("Buggy task")).toBeInTheDocument();
      expect(screen.queryByText("Feature task")).not.toBeInTheDocument();
      expect(screen.getByText("Label: bug")).toBeInTheDocument();
    });

    it("clears the label filter from the chip's clear button", () => {
      currentSearchParams = new URLSearchParams("label=bug");
      const tasks = [makeTask({ id: "t1", title: "Buggy task", labels: ["bug"] })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);

      fireEvent.click(screen.getByRole("button", { name: "Clear label filter: bug" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
    });

    it("reports an empty result from the label filter", () => {
      currentSearchParams = new URLSearchParams("label=bug");
      const tasks = [makeTask({ id: "t1", title: "Feature task", labels: ["feature"] })];
      render(
        <TaskListSection projectId="p1" tasks={tasks} backlogs={[]} statusFilter="all" />,
      );

      expect(screen.getByText('No tasks labeled "bug".')).toBeInTheDocument();
    });

    it("filters the board view by the same label", () => {
      currentSearchParams = new URLSearchParams("label=bug");
      const tasks = [
        makeTask({ id: "t1", title: "Buggy task", labels: ["bug"] }),
        makeTask({ id: "t2", title: "Feature task", labels: ["feature"] }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} statusFilter="all" />);

      expect(screen.getByRole("link", { name: "Buggy task" })).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Feature task" })).not.toBeInTheDocument();
    });
  });

  describe("Due date filter and state (issue #148)", () => {
    // Wednesday 2026-08-05, so the week runs through Sunday 2026-08-09 — the
    // same fixture lib/dashboard.test.ts's dueStatus tests use.
    const today = "2026-08-05";

    it("marks a task due before today as Overdue, in destructive text", () => {
      const tasks = [makeTask({ id: "t1", title: "Late task", dueOn: "2026-08-04" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} today={today} />);
      showListView();

      expect(screen.getByText("Overdue Aug 4, 2026")).toHaveClass("text-destructive");
    });

    it("shows a task due today as Due, not Overdue", () => {
      const tasks = [makeTask({ id: "t1", title: "Due today", dueOn: "2026-08-05" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} today={today} />);
      showListView();

      expect(screen.getByText("Due Aug 5, 2026")).not.toHaveClass("text-destructive");
    });

    it("shows the same Overdue state in the board view", () => {
      const tasks = [makeTask({ id: "t1", title: "Late task", dueOn: "2026-08-04" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} today={today} />);

      expect(screen.getByText("Overdue Aug 4, 2026")).toBeInTheDocument();
    });

    it("pushes ?due= when the due date filter changes", async () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} today={today} />);

      fireEvent.click(screen.getByRole("combobox", { name: "Due date" }));
      fireEvent.click(await screen.findByRole("option", { name: "Overdue" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?due=overdue");
    });

    it("drops ?due= back out of the query string when it returns to All due dates", async () => {
      currentSearchParams = new URLSearchParams("due=overdue");
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} today={today} />);

      fireEvent.click(screen.getByRole("combobox", { name: "Due date" }));
      fireEvent.click(await screen.findByRole("option", { name: "All due dates" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
    });

    it("narrows to overdue tasks when ?due=overdue is set", () => {
      currentSearchParams = new URLSearchParams("due=overdue");
      const tasks = [
        makeTask({ id: "t1", title: "Late task", dueOn: "2026-08-04" }),
        makeTask({ id: "t2", title: "Future task", dueOn: "2026-08-10" }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} today={today} />);

      expect(screen.getByRole("link", { name: "Late task" })).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Future task" })).not.toBeInTheDocument();
    });

    it("narrows to tasks due through the end of this week when ?due=dueSoon is set", () => {
      currentSearchParams = new URLSearchParams("due=dueSoon");
      const tasks = [
        makeTask({ id: "t1", title: "End of week", dueOn: "2026-08-09" }),
        makeTask({ id: "t2", title: "Next week", dueOn: "2026-08-10" }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} today={today} />);

      expect(screen.getByRole("link", { name: "End of week" })).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Next week" })).not.toBeInTheDocument();
    });

    it("narrows to tasks without a due date when ?due=undated is set", () => {
      currentSearchParams = new URLSearchParams("due=undated");
      const tasks = [
        makeTask({ id: "t1", title: "No due date", dueOn: null }),
        makeTask({ id: "t2", title: "Has due date", dueOn: "2026-08-10" }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} today={today} />);

      expect(screen.getByRole("link", { name: "No due date" })).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Has due date" })).not.toBeInTheDocument();
    });

    it("reports an empty result from the due date filter", () => {
      currentSearchParams = new URLSearchParams("due=overdue");
      render(
        <TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" today={today} />,
      );

      expect(screen.getByText("No overdue tasks.")).toBeInTheDocument();
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
      "/api/v1/tasks/t1/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: "b1" }) }),
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/tasks/t2/assign-backlog",
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
      "/api/v1/tasks/t1/assign-backlog",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: null }) }),
    );
  });

  it("offers selection even when the project has no backlogs (issue #149)", () => {
    const tasks = [makeTask({ id: "t1", title: "Only task", backlogId: null })];
    render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
    showListView();
    expect(screen.getByRole("checkbox", { name: "Select Only task" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: "Select Only task" }));
    expect(screen.getByText("1 selected")).toBeInTheDocument();
    // "Assign to backlog" stays hidden — there is nowhere to assign it to —
    // but the rest of the bulk actions don't depend on a backlog existing.
    expect(screen.queryByRole("combobox", { name: "Backlog to assign" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Close selected" })).toBeInTheDocument();
  });

  describe("Bulk actions (issue #149)", () => {
    it("selects and deselects every visible task with the top-level select-all checkbox", () => {
      const tasks = [
        makeTask({ id: "t1", title: "Task A", backlogId: null }),
        makeTask({ id: "t2", title: "Task B", backlogId: null }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();

      fireEvent.click(screen.getByRole("checkbox", { name: "Select all tasks" }));
      expect(screen.getByText("2 selected")).toBeInTheDocument();
      expect(screen.getByRole("checkbox", { name: "Select Task A" })).toBeChecked();
      expect(screen.getByRole("checkbox", { name: "Select Task B" })).toBeChecked();

      fireEvent.click(screen.getByRole("checkbox", { name: "Select all tasks" }));
      expect(screen.queryByText(/selected/)).not.toBeInTheDocument();
    });

    it("shows the select-all checkbox as indeterminate once some but not all tasks are selected", () => {
      const tasks = [
        makeTask({ id: "t1", title: "Task A", backlogId: null }),
        makeTask({ id: "t2", title: "Task B", backlogId: null }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();

      fireEvent.click(screen.getByRole("checkbox", { name: "Select Task A" }));

      const selectAll = screen.getByRole("checkbox", { name: "Select all tasks" }) as HTMLInputElement;
      expect(selectAll.indeterminate).toBe(true);
      expect(selectAll.checked).toBe(false);
    });

    it("selects only one backlog group's tasks from that group's own select-all checkbox", () => {
      const tasks = [
        makeTask({ id: "t1", title: "Sprint task", backlogId: "b1" }),
        makeTask({ id: "t2", title: "Icebox task", backlogId: "b2" }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);
      showListView();

      fireEvent.click(screen.getByRole("checkbox", { name: "Select all in Sprint 1" }));

      expect(screen.getByRole("checkbox", { name: "Select Sprint task" })).toBeChecked();
      expect(screen.getByRole("checkbox", { name: "Select Icebox task" })).not.toBeChecked();
      expect(screen.getByText("1 selected")).toBeInTheDocument();
    });

    it("applies a bulk priority change to every selected task", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      const tasks = [
        makeTask({ id: "t1", title: "Task A", backlogId: null }),
        makeTask({ id: "t2", title: "Task B", backlogId: null }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select all tasks" }));

      fireEvent.click(screen.getByRole("combobox", { name: "Priority to set" }));
      fireEvent.click(await screen.findByRole("option", { name: "Urgent" }));
      fireEvent.click(screen.getByRole("button", { name: "Set priority" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/tasks/t1",
        expect.objectContaining({ method: "PATCH", body: JSON.stringify({ priority: "urgent" }) }),
      );
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/tasks/t2",
        expect.objectContaining({ method: "PATCH", body: JSON.stringify({ priority: "urgent" }) }),
      );
      expect(screen.queryByText(/selected/)).not.toBeInTheDocument();
    });

    it("applies a bulk progress change to every selected task", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      const tasks = [makeTask({ id: "t1", title: "Task A", backlogId: null })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Task A" }));

      fireEvent.click(screen.getByRole("combobox", { name: "Progress to set" }));
      fireEvent.click(await screen.findByRole("option", { name: "On hold" }));
      fireEvent.click(screen.getByRole("button", { name: "Set progress" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/tasks/t1",
        expect.objectContaining({ method: "PATCH", body: JSON.stringify({ progress: "on_hold" }) }),
      );
    });

    it("closes every selected task in bulk", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      const tasks = [makeTask({ id: "t1", title: "Task A", backlogId: null, status: "open" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Task A" }));

      fireEvent.click(screen.getByRole("button", { name: "Close selected" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/tasks/t1/close",
        expect.objectContaining({ method: "POST" }),
      );
    });

    it("reopens every selected task in bulk", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      const tasks = [makeTask({ id: "t1", title: "Task A", backlogId: null, status: "closed" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} statusFilter="all" />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Task A" }));

      fireEvent.click(screen.getByRole("button", { name: "Reopen selected" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/tasks/t1/reopen",
        expect.objectContaining({ method: "POST" }),
      );
    });

    it("reports a partial failure with counts and narrows the selection to the failed tasks", async () => {
      vi.mocked(fetch).mockImplementation((input) =>
        String(input).includes("/t2")
          ? Promise.resolve(new Response(null, { status: 500 }))
          : Promise.resolve(new Response(null, { status: 200 })),
      );
      const tasks = [
        makeTask({ id: "t1", title: "Task A", backlogId: null }),
        makeTask({ id: "t2", title: "Task B", backlogId: null }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select all tasks" }));

      fireEvent.click(screen.getByRole("button", { name: "Close selected" }));

      expect(await screen.findByText("1 of 2 tasks failed to update.")).toBeInTheDocument();
      expect(screen.getByText("1 selected")).toBeInTheDocument();
      expect(screen.getByRole("checkbox", { name: "Select Task B" })).toBeChecked();
      expect(screen.getByRole("checkbox", { name: "Select Task A" })).not.toBeChecked();
    });

    it("reports a full failure without narrowing, since nothing succeeded", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 500 }));
      const tasks = [makeTask({ id: "t1", title: "Task A", backlogId: null })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Task A" }));

      fireEvent.click(screen.getByRole("button", { name: "Close selected" }));

      expect(await screen.findByText("Failed to update 1 task.")).toBeInTheDocument();
      expect(screen.getByText("1 selected")).toBeInTheDocument();
    });

    it("prunes a selection that falls out of view once the task list narrows under it", () => {
      const tasks = [
        makeTask({ id: "t1", title: "Keep", backlogId: null }),
        makeTask({ id: "t2", title: "Drop", backlogId: null }),
      ];
      const { rerender } = render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Keep" }));
      fireEvent.click(screen.getByRole("checkbox", { name: "Select Drop" }));
      expect(screen.getByText("2 selected")).toBeInTheDocument();

      // Simulates the filtered/re-fetched list the API would return once the
      // selection's owning filter narrows — "Drop" is no longer among the
      // tasks passed down at all.
      rerender(
        <TaskListSection
          projectId="p1"
          tasks={[makeTask({ id: "t1", title: "Keep", backlogId: null })]}
          backlogs={[]}
        />,
      );

      expect(screen.getByText("1 selected")).toBeInTheDocument();
    });
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
      "/api/v1/projects/p1/tasks",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          title: "Fix bug",
          description: "",
          backlogId: null,
          startDate: "2026-08-15T00:00:00Z",
          dueOn: null,
          priority: "medium",
          size: "m",
          progress: "not_started",
        }),
      }),
    );
    expect(screen.queryByRole("form", { name: "New task" })).not.toBeInTheDocument();
  });

  // Size is settable at creation time, not only from the edit form: if the
  // only way to size a task is to go back and edit it, nothing gets sized and
  // points-based velocity stays a flat multiple of the task count.
  it("creates a task with the size picked in the form", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) });
    vi.stubGlobal("fetch", fetchMock);
    render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "New task" }));
    fireEvent.change(withinNewTaskForm().getByLabelText("Title"), { target: { value: "Big job" } });

    fireEvent.click(withinNewTaskForm().getByRole("combobox", { name: "Size" }));
    fireEvent.click(await screen.findByRole("option", { name: "XL (8 pts)" }));

    fireEvent.click(withinNewTaskForm().getByRole("button", { name: "Create task" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.size).toBe("xl");
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
      "/api/v1/projects/p1/tasks",
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
      "/api/v1/projects/p1/tasks/order",
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

  describe("Drag feedback and empty backlog drop targets (issue #154)", () => {
    it("dims the row being dragged and highlights the row under the pointer as the drop target", () => {
      const tasks = [
        makeTask({ id: "t1", title: "First", backlogId: "b1" }),
        makeTask({ id: "t2", title: "Second", backlogId: "b1" }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
      showListView();

      const firstRow = screen.getByRole("link", { name: /First/ });
      const secondRow = screen.getByRole("link", { name: /Second/ });

      fireEvent.dragStart(screen.getByTestId("drag-handle-t1"));
      expect(firstRow).toHaveClass("opacity-50");
      expect(secondRow).not.toHaveClass("opacity-50");

      fireEvent.dragOver(secondRow);
      expect(secondRow).toHaveClass("ring-2");
      expect(firstRow).not.toHaveClass("ring-2");

      fireEvent.dragEnd(screen.getByTestId("drag-handle-t1"));
      expect(firstRow).not.toHaveClass("opacity-50");
      expect(secondRow).not.toHaveClass("ring-2");
    });

    it("renders an empty backlog as a labeled drop target while sorted manually with no backlog filter", () => {
      // Unclassified is also empty here and gets the same placeholder, so
      // this is scoped to the Icebox group specifically.
      const tasks = [makeTask({ id: "t1", title: "Filed task", backlogId: "b1" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);
      showListView();

      const iceboxGroup = within(screen.getByText("Icebox (0)").closest("div")!);
      expect(iceboxGroup.getByText("Drag a task here to move it into this backlog.")).toBeInTheDocument();
    });

    it("moves a task into an empty backlog by dropping it on the placeholder", async () => {
      vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
      const tasks = [makeTask({ id: "t1", title: "Filed task", backlogId: "b1" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog, otherBacklog]} />);
      showListView();

      const iceboxGroup = within(screen.getByText("Icebox (0)").closest("div")!);
      fireEvent.dragStart(screen.getByTestId("drag-handle-t1"));
      const dropZone = iceboxGroup.getByText("Drag a task here to move it into this backlog.");
      fireEvent.dragOver(dropZone);
      fireEvent.drop(dropZone);

      // A cross-backlog move is a reorder, not a create/bulk action — it
      // updates optimistically rather than triggering a router.refresh().
      await waitFor(() =>
        expect(fetch).toHaveBeenCalledWith(
          "/api/v1/tasks/t1/assign-backlog",
          expect.objectContaining({ method: "POST", body: JSON.stringify({ backlogId: "b2" }) }),
        ),
      );
    });

    it("does not add an empty backlog group once a backlog filter narrows the view", () => {
      const tasks = [makeTask({ id: "t1", title: "Filed task", backlogId: "b1" })];
      render(
        <TaskListSection
          projectId="p1"
          tasks={tasks}
          backlogs={[backlog, otherBacklog]}
          backlogFilter="b1"
        />,
      );
      showListView();
      expect(screen.queryByText("Icebox (0)")).not.toBeInTheDocument();
    });

    it("hides the empty-backlog drop target while sorted by anything other than manual", () => {
      const tasks = [makeTask({ id: "t1", title: "Filed task", backlogId: "b1" })];
      render(
        <TaskListSection
          projectId="p1"
          tasks={tasks}
          backlogs={[backlog, otherBacklog]}
          sort="priority"
        />,
      );
      showListView();
      expect(screen.queryByText("Icebox (0)")).not.toBeInTheDocument();
    });
  });

  describe("Reorder hint while sort isn't manual (issue #154)", () => {
    it("explains why the drag handle and move buttons are gone once sorted by something else", () => {
      const tasks = [makeTask({ id: "t1", title: "Only task", backlogId: "b1" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} sort="priority" />);
      showListView();
      expect(
        screen.getByText("Switch sort to Manual order to drag or reorder tasks."),
      ).toBeInTheDocument();
    });

    it("shows no such hint while already sorted manually", () => {
      const tasks = [makeTask({ id: "t1", title: "Only task", backlogId: "b1" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
      showListView();
      expect(
        screen.queryByText("Switch sort to Manual order to drag or reorder tasks."),
      ).not.toBeInTheDocument();
    });
  });

  describe("Row density (issue #154)", () => {
    it("shows the status badge only when the task is closed", () => {
      const tasks = [
        makeTask({ id: "t1", title: "Open task", status: "open" }),
        makeTask({ id: "t2", title: "Closed task", status: "closed" }),
      ];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} statusFilter="all" />);
      showListView();
      expect(screen.queryByText("Open")).not.toBeInTheDocument();
      expect(screen.getByText("Closed")).toBeInTheDocument();
    });

    it("caps labels at three and rolls the rest into a +n badge", () => {
      const tasks = [makeTask({ id: "t1", title: "Task", labels: ["a", "b", "c", "d", "e"] })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[]} />);
      showListView();
      expect(screen.getByRole("button", { name: "a" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "b" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "c" })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "d" })).not.toBeInTheDocument();
      expect(screen.getByText("+2")).toBeInTheDocument();
    });
  });

  describe("View mode in the URL (issue #153)", () => {
    it("opens in Board by default when no initialView is passed", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
      expect(screen.getByRole("button", { name: "Board" })).toHaveAttribute("aria-pressed", "true");
    });

    it("opens in the view passed as initialView", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} initialView="timeline" />);
      expect(screen.getByRole("button", { name: "Timeline" })).toHaveAttribute("aria-pressed", "true");
    });

    it("pushes ?view= when switching to List", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
      showListView();
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?view=list");
    });

    it("pushes ?view= when switching to Timeline", () => {
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} />);
      fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?view=timeline");
    });

    it("drops ?view= back out of the query string when switching back to Board", () => {
      currentSearchParams = new URLSearchParams("view=list");
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} initialView="list" />);
      fireEvent.click(screen.getByRole("button", { name: "Board" }));
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks");
    });

    it("keeps the other filters in the query string when switching view", () => {
      currentSearchParams = new URLSearchParams("status=all");
      render(<TaskListSection projectId="p1" tasks={[]} backlogs={[]} statusFilter="all" />);
      showListView();
      expect(push).toHaveBeenCalledWith("/projects/p1/tasks?status=all&view=list");
    });

    it("switches the rendered view immediately, without waiting on a navigation", () => {
      const tasks = [makeTask({ id: "t1", title: "Only task", backlogId: "b1" })];
      render(<TaskListSection projectId="p1" tasks={tasks} backlogs={[backlog]} />);
      showListView();
      expect(screen.getByRole("button", { name: "List" })).toHaveAttribute("aria-pressed", "true");
      expect(screen.getByText("Sprint 1 (1)")).toBeInTheDocument();
    });
  });
});
