import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within, act } from "@testing-library/react";
import type { Backlog, LinkedGitlabProject } from "@/types";
import { BacklogListSection } from "./BacklogListSection";

const refresh = vi.fn();
const push = vi.fn();
// The filters are the URL (issue #151, mirroring TaskListSection), so what
// these tests assert about filtering is the query pushed and, for the
// client-only `sort=dueOn`/`?q=`, the order/list actually rendered.
let currentSearchParams = new URLSearchParams();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh, push }),
  usePathname: () => "/projects/p1/backlogs",
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
  baseBranch: "",
  allowedScope: "",
  forbiddenScope: "",
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

/** Two links so "the project default" and "this backlog's own" are distinct
 *  (issue #180). */
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
  {
    id: "l2",
    gitlabConnectionId: "c1",
    gitlabProjectId: 200,
    pathWithNamespace: "group/other",
    name: "other",
    webUrl: "https://gitlab.example.com/group/other",
    syncScope: "all",
    syncLabels: [],
    isDefault: false,
    initialImportStatus: "completed",
    lastSyncedAt: null,
    webhookStatus: "registered",
    webhookRegisteredAt: null,
    webhookError: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  },
];

/** bodyOf reads back the JSON a mocked fetch call was given. */
function bodyOf(call: Parameters<typeof fetch>) {
  return JSON.parse(String((call[1] as RequestInit).body));
}

const otherBacklog: Backlog = {
  ...backlog,
  id: "b2",
  name: "Icebox",
  position: 1,
};

/** The Board is the default view mode, so tests about the List mode's rows
 *  switch to it first. */
function showList() {
  fireEvent.click(screen.getByRole("button", { name: "List" }));
}

describe("BacklogListSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    push.mockClear();
    currentSearchParams = new URLSearchParams();
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows an empty state with zero backlogs", () => {
    render(<BacklogListSection projectId="p1" backlogs={[]} />);
    expect(screen.getByText("No backlogs yet.")).toBeInTheDocument();
  });

  it("shows a load error, without the view toggle or filter row, but keeps New backlog reachable", () => {
    render(<BacklogListSection projectId="p1" backlogs={[]} error />);
    expect(screen.getByText("Failed to load backlogs. Try refreshing the page.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "List" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Priority")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New backlog" })).toBeInTheDocument();
  });

  it("lists backlogs with a link to the single view", () => {
    render(<BacklogListSection projectId="p1" backlogs={[{ ...backlog, taskCount: 3 }]} />);
    showList();
    const link = screen.getByRole("link", { name: "Sprint 1" });
    expect(link).toHaveAttribute("href", "/projects/p1/backlogs/b1");
  });

  it("shows each row's closed/total completion, the same reading the Board mode's cards use", () => {
    render(
      <BacklogListSection
        projectId="p1"
        backlogs={[{ ...backlog, taskCount: 5, closedTaskCount: 2 }]}
      />,
    );
    showList();
    expect(screen.getByText("2/5 closed")).toBeInTheDocument();
  });

  it("reports \"No tasks\" for a backlog with none, rather than a 0/0 ratio", () => {
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
    showList();
    expect(screen.getByText("No tasks")).toBeInTheDocument();
  });

  it("shows the Board view mode by default, grouped by progress", () => {
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
    const notStarted = screen.getByRole("region", { name: "Not started backlogs" });
    expect(within(notStarted).getByRole("link", { name: "Sprint 1" })).toBeInTheDocument();
  });

  it("creates a new backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ ...backlog, id: "b2" }), { status: 201 }));
    render(<BacklogListSection projectId="p1" backlogs={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "New backlog" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Sprint 2" } });
    fireEvent.click(screen.getByRole("button", { name: "Create backlog" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/projects/p1/backlogs",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("sends the entered base branch when creating a backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ ...backlog, id: "b2" }), { status: 201 }));
    render(<BacklogListSection projectId="p1" backlogs={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "New backlog" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Sprint 2" } });
    fireEvent.change(screen.getByLabelText("Base branch"), { target: { value: "main" } });
    fireEvent.click(screen.getByRole("button", { name: "Create backlog" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(bodyOf(vi.mocked(fetch).mock.calls[0])).toMatchObject({
      baseBranch: "main",
    });
  });

  it("sends the edited base branch with the rest of the backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(backlog), { status: 200 }));
    render(<BacklogListSection projectId="p1" backlogs={[{ ...backlog, baseBranch: "main" }]} />);
    showList();

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByLabelText("Base branch")).toHaveValue("main");
    fireEvent.change(screen.getByLabelText("Base branch"), { target: { value: "develop" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(bodyOf(vi.mocked(fetch).mock.calls[0])).toMatchObject({
      baseBranch: "develop",
    });
  });

  // A backlog can name its own GitLab project for new issues (issue #180).
  // The field only exists where there is something to choose between, and an
  // edit that doesn't touch it must not reset it.
  describe("GitLab project for new issues", () => {
    it("hides the field when the project has no linked GitLab project", () => {
      render(<BacklogListSection projectId="p1" backlogs={[]} />);
      fireEvent.click(screen.getByRole("button", { name: "New backlog" }));
      expect(
        screen.queryByLabelText("GitLab project for new issues"),
      ).not.toBeInTheDocument();
    });

    it("defaults a new backlog to the project default", async () => {
      vi.mocked(fetch).mockResolvedValue(
        new Response(JSON.stringify(backlog), { status: 201 }),
      );
      render(
        <BacklogListSection projectId="p1" backlogs={[]} links={links} />,
      );

      fireEvent.click(screen.getByRole("button", { name: "New backlog" }));
      expect(
        screen.getByLabelText("GitLab project for new issues"),
      ).toHaveTextContent("Project default (group/demo)");
      fireEvent.change(screen.getByLabelText("Name"), {
        target: { value: "Sprint 2" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Create backlog" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(bodyOf(vi.mocked(fetch).mock.calls[0])).toMatchObject({
        defaultLinkedGitlabProjectId: null,
      });
    });

    it("keeps the backlog's own link through an unrelated edit", async () => {
      vi.mocked(fetch).mockResolvedValue(
        new Response(JSON.stringify(backlog), { status: 200 }),
      );
      render(
        <BacklogListSection
          projectId="p1"
          backlogs={[{ ...backlog, defaultLinkedGitlabProjectId: "l2" }]}
          links={links}
        />,
      );
      showList();

      fireEvent.click(screen.getByRole("button", { name: "Edit" }));
      expect(
        screen.getByLabelText("GitLab project for new issues"),
      ).toHaveTextContent("group/other");
      fireEvent.change(screen.getByLabelText("Name"), {
        target: { value: "Renamed" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() => expect(refresh).toHaveBeenCalled());
      expect(bodyOf(vi.mocked(fetch).mock.calls[0])).toMatchObject({
        name: "Renamed",
        defaultLinkedGitlabProjectId: "l2",
      });
    });
  });

  it("renames a backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ ...backlog, name: "Renamed" }), { status: 200 }));
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
    showList();

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/backlogs/b1",
      expect.objectContaining({ method: "PATCH" }),
    );
  });

  it("sends the edited schedule with the rest of the backlog", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(backlog), { status: 200 }));
    const scheduled = { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" };
    render(<BacklogListSection projectId="p1" backlogs={[scheduled]} />);
    showList();

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
    render(<BacklogListSection projectId="p1" backlogs={[scheduled]} />);
    showList();
    expect(screen.getByText(/Aug 1, 2026\s*–\s*Aug 31, 2026/)).toBeInTheDocument();
  });

  it("switches to the timeline view mode and back", async () => {
    const scheduled = { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" };
    render(<BacklogListSection projectId="p1" backlogs={[scheduled]} />);

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
    render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
    showList();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(
      screen.getByText("Its tasks will move to Unclassified. Delete this backlog?"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/backlogs/b1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("moves a backlog down with the move-down button, updating the display order optimistically", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 200 }));
    render(<BacklogListSection projectId="p1" backlogs={[backlog, otherBacklog]} />);
    showList();

    fireEvent.click(screen.getByRole("button", { name: "Move Sprint 1 down" }));

    const names = screen
      .getAllByRole("link", { name: /^(Sprint 1|Icebox)$/ })
      .map((el) => el.textContent);
    expect(names).toEqual(["Icebox", "Sprint 1"]);
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/projects/p1/backlogs/order",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({ backlogIds: ["b2", "b1"] }),
      }),
    );
  });

  it("reverts the order and shows an error when the reorder request fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "backlog_ids_mismatch", message: "backlogIds must match" } }), {
        status: 400,
      }),
    );
    render(<BacklogListSection projectId="p1" backlogs={[backlog, otherBacklog]} />);
    showList();

    fireEvent.click(screen.getByRole("button", { name: "Move Sprint 1 down" }));

    expect(await screen.findByText("backlogIds must match")).toBeInTheDocument();
    const names = screen
      .getAllByRole("link", { name: /^(Sprint 1|Icebox)$/ })
      .map((el) => el.textContent);
    expect(names).toEqual(["Sprint 1", "Icebox"]);
  });

  it("disables the move buttons at the ends of the list", () => {
    render(<BacklogListSection projectId="p1" backlogs={[backlog, otherBacklog]} />);
    showList();
    expect(screen.getByRole("button", { name: "Move Sprint 1 up" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Move Icebox down" })).toBeDisabled();
  });

  describe("filters and sort (issue #151)", () => {
    it("pushes ?priority= alongside the other filters rather than replacing them", async () => {
      currentSearchParams = new URLSearchParams("progress=on_hold");
      render(
        <BacklogListSection projectId="p1" backlogs={[backlog]} progressFilter="on_hold" />,
      );

      fireEvent.click(screen.getByRole("combobox", { name: "Priority" }));
      fireEvent.click(await screen.findByRole("option", { name: "Urgent" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs?progress=on_hold&priority=urgent");
    });

    it("drops ?priority= back out of the query string when it returns to All priorities", async () => {
      currentSearchParams = new URLSearchParams("priority=urgent");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} priorityFilter="urgent" />);

      fireEvent.click(screen.getByRole("combobox", { name: "Priority" }));
      fireEvent.click(await screen.findByRole("option", { name: "All priorities" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs");
    });

    it("pushes ?progress= alongside the other filters rather than replacing them", async () => {
      currentSearchParams = new URLSearchParams("priority=urgent");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} priorityFilter="urgent" />);

      fireEvent.click(screen.getByRole("combobox", { name: "Progress" }));
      fireEvent.click(await screen.findByRole("option", { name: "On hold" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs?priority=urgent&progress=on_hold");
    });

    it("shows the applied filters from the URL query the screen was opened with", () => {
      render(
        <BacklogListSection
          projectId="p1"
          backlogs={[backlog]}
          priorityFilter="high"
          progressFilter="on_hold"
          sort="priority"
        />,
      );
      expect(screen.getByRole("combobox", { name: "Priority" })).toHaveTextContent("High");
      expect(screen.getByRole("combobox", { name: "Progress" })).toHaveTextContent("On hold");
      expect(screen.getByRole("combobox", { name: "Sort" })).toHaveTextContent("Priority");
    });

    it("pushes ?sort=dueOn, sorting the client-side list it can't ask the API for", async () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);

      fireEvent.click(screen.getByRole("combobox", { name: "Sort" }));
      fireEvent.click(screen.getByRole("option", { name: "Due date" }));

      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs?sort=dueOn");
    });

    it("sorts by due date client-side, undated backlogs last", () => {
      const dated = { ...backlog, id: "b1", name: "Sprint 1", dueOn: "2026-08-15T00:00:00Z" };
      const undated = { ...otherBacklog, id: "b2", name: "Icebox", dueOn: null };
      render(
        <BacklogListSection projectId="p1" backlogs={[undated, dated]} sort="dueOn" />,
      );
      showList();
      const names = screen
        .getAllByRole("link", { name: /^(Sprint 1|Icebox)$/ })
        .map((el) => el.textContent);
      expect(names).toEqual(["Sprint 1", "Icebox"]);
    });

    it("hides the move buttons and drag handle while a priority filter narrows the list", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} priorityFilter="medium" />);
      showList();
      expect(screen.queryByRole("button", { name: "Move Sprint 1 up" })).not.toBeInTheDocument();
    });

    it("hides the move buttons and drag handle during a non-manual sort", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} sort="priority" />);
      showList();
      expect(screen.queryByRole("button", { name: "Move Sprint 1 up" })).not.toBeInTheDocument();
    });

    it("shows Clear filters once a filter differs from the default, and clears the whole query", () => {
      currentSearchParams = new URLSearchParams("priority=urgent");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} priorityFilter="urgent" />);

      const clearButton = screen.getByRole("button", { name: "Clear filters" });
      fireEvent.click(clearButton);
      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs");
    });

    it("pushes the search text as ?q= once the debounce elapses, matched client-side", () => {
      vi.useFakeTimers();
      render(<BacklogListSection projectId="p1" backlogs={[backlog, otherBacklog]} />);

      fireEvent.change(screen.getByRole("textbox", { name: "Search backlogs" }), {
        target: { value: "sprint" },
      });
      expect(push).not.toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(300);
      });

      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs?q=sprint");
    });

    it("matches the search text against a backlog's name, case-insensitively", () => {
      currentSearchParams = new URLSearchParams("q=SPRINT");
      render(<BacklogListSection projectId="p1" backlogs={[backlog, otherBacklog]} />);
      showList();
      expect(screen.getByRole("link", { name: /Sprint 1/ })).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: /Icebox/ })).not.toBeInTheDocument();
    });

    it("reports an empty result from the search term, distinct from an empty project", () => {
      currentSearchParams = new URLSearchParams("q=nomatch");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
      expect(screen.getByText('No backlogs match "nomatch".')).toBeInTheDocument();
    });

    it("reports an empty result from the priority filter, distinct from an empty project", () => {
      render(<BacklogListSection projectId="p1" backlogs={[]} priorityFilter="urgent" />);
      expect(screen.getByText("No urgent priority backlogs.")).toBeInTheDocument();
    });

    it("still shows the empty-project message with no filters active", () => {
      render(<BacklogListSection projectId="p1" backlogs={[]} />);
      expect(screen.getByText("No backlogs yet.")).toBeInTheDocument();
    });
  });

  describe("Unclassified row (issue #152)", () => {
    it("shows a trailing Unclassified row with its count, linking to the Task collection", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={4} />);
      showList();
      const link = screen.getByRole("link", { name: /Unclassified/ });
      expect(link).toHaveTextContent("(4)");
      expect(link).toHaveAttribute("href", "/projects/p1/tasks?backlog=unclassified");
    });

    it("hides the Unclassified row when there are no unclassified tasks", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={0} />);
      showList();
      expect(screen.queryByRole("link", { name: /Unclassified/ })).not.toBeInTheDocument();
    });

    it("hides the Unclassified row by default when the caller doesn't pass a count", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
      showList();
      expect(screen.queryByRole("link", { name: /Unclassified/ })).not.toBeInTheDocument();
    });

    it("shows the Unclassified row even when the filtered backlog list itself is empty", () => {
      currentSearchParams = new URLSearchParams("q=unclass");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={2} />);
      showList();
      expect(screen.getByRole("link", { name: /Unclassified/ })).toBeInTheDocument();
      expect(screen.queryByText('No backlogs match "unclass".')).not.toBeInTheDocument();
    });

    it("has no grip, move or Edit/Delete controls on the Unclassified row", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={4} />);
      showList();
      expect(screen.queryByRole("button", { name: "Move Unclassified up" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Move Unclassified down" })).not.toBeInTheDocument();
      // Sprint 1 still has its own Edit/Delete; Unclassified doesn't add a second pair.
      expect(screen.getAllByRole("button", { name: "Edit" })).toHaveLength(1);
      expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(1);
    });

    it("hides the Unclassified row while a priority filter is active, since it has no priority", () => {
      render(
        <BacklogListSection
          projectId="p1"
          backlogs={[backlog]}
          priorityFilter="medium"
          unclassifiedCount={4}
        />,
      );
      showList();
      expect(screen.queryByRole("link", { name: /Unclassified/ })).not.toBeInTheDocument();
    });

    it("hides the Unclassified row while a progress filter is active, since it has no progress", () => {
      render(
        <BacklogListSection
          projectId="p1"
          backlogs={[backlog]}
          progressFilter="on_hold"
          unclassifiedCount={4}
        />,
      );
      showList();
      expect(screen.queryByRole("link", { name: /Unclassified/ })).not.toBeInTheDocument();
    });

    it("hides the Unclassified row once a name search doesn't match it", () => {
      currentSearchParams = new URLSearchParams("q=sprint");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={4} />);
      showList();
      expect(screen.queryByRole("link", { name: /Unclassified/ })).not.toBeInTheDocument();
    });

    it("keeps showing the Unclassified row when the search matches its own name", () => {
      currentSearchParams = new URLSearchParams("q=unclass");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={4} />);
      showList();
      expect(screen.getByRole("link", { name: /Unclassified/ })).toBeInTheDocument();
    });

    it("does not show the Unclassified row in Board mode", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} unclassifiedCount={4} />);
      expect(screen.queryByRole("link", { name: /Unclassified/ })).not.toBeInTheDocument();
    });
  });

  describe("View mode in the URL (issue #153)", () => {
    it("opens in Board by default when no initialView is passed", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
      expect(screen.getByRole("button", { name: "Board" })).toHaveAttribute("aria-pressed", "true");
    });

    it("opens in the view passed as initialView", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} initialView="timeline" />);
      expect(screen.getByRole("button", { name: "Timeline" })).toHaveAttribute("aria-pressed", "true");
    });

    it("pushes ?view= when switching to List", () => {
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} />);
      showList();
      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs?view=list");
    });

    it("drops ?view= back out of the query string when switching back to Board", () => {
      currentSearchParams = new URLSearchParams("view=list");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} initialView="list" />);
      fireEvent.click(screen.getByRole("button", { name: "Board" }));
      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs");
    });

    it("keeps the other filters in the query string when switching view", () => {
      currentSearchParams = new URLSearchParams("priority=urgent");
      render(<BacklogListSection projectId="p1" backlogs={[backlog]} priorityFilter="urgent" />);
      showList();
      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs?priority=urgent&view=list");
    });
  });
});
