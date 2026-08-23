import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Backlog, LinkedGitlabProject, Task } from "@/types";
import { BacklogDetail } from "./BacklogDetail";

const push = vi.fn();
const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh }),
}));

const project = { id: "p1", name: "Alpha" };

/** Two links so the project default and a backlog's own are distinct. */
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

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "The first sprint",
  position: 0,
  startDate: null,
  dueOn: null,
  priority: "medium",
  progress: "not_started",
  defaultLinkedGitlabProjectId: null,
  baseBranch: "",
  allowedScope: "",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  forbiddenScope: "",
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-02T00:00:00Z",
};

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    title: "Fix the bug",
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
    position: 0,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

describe("BacklogDetail", () => {
  beforeEach(() => {
    push.mockClear();
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows identity and attributes", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(
      screen.getByRole("heading", { name: "Sprint 1" }),
    ).toBeInTheDocument();
    expect(screen.getByText("The first sprint")).toBeInTheDocument();
  });

  it("shows the planned period, or says it is unset", () => {
    const { rerender } = render(
      <BacklogDetail backlog={backlog} project={project} tasks={[]} />,
    );
    // Start date, due date, base branch, allowed scope, and forbidden scope
    // each say "Not set" when unset.
    expect(screen.getAllByText("Not set")).toHaveLength(5);

    rerender(
      <BacklogDetail
        backlog={{
          ...backlog,
          startDate: "2026-08-01T00:00:00Z",
          dueOn: "2026-08-31T00:00:00Z",
        }}
        project={project}
        tasks={[]}
      />,
    );
    expect(screen.getByText("Aug 1, 2026")).toBeInTheDocument();
    expect(screen.getByText("Aug 31, 2026")).toBeInTheDocument();
  });

  // Where a task filed here gets its GitLab issue created (issue #180): the
  // backlog's own link, or the project's default when it names none.
  it("names the GitLab project new issues go to, and where that came from", () => {
    const { rerender } = render(
      <BacklogDetail
        backlog={backlog}
        project={project}
        tasks={[]}
        links={links}
      />,
    );
    expect(screen.getByText("group/demo")).toBeInTheDocument();
    expect(screen.getByText("(project default)")).toBeInTheDocument();

    rerender(
      <BacklogDetail
        backlog={{ ...backlog, defaultLinkedGitlabProjectId: "l2" }}
        project={project}
        tasks={[]}
        links={links}
      />,
    );
    expect(screen.getByText("group/other")).toBeInTheDocument();
    expect(screen.queryByText("(project default)")).not.toBeInTheDocument();
  });

  it("shows the base branch, or says it is unset", () => {
    const { rerender } = render(
      <BacklogDetail backlog={backlog} project={project} tasks={[]} />,
    );
    expect(screen.getByText("Base branch")).toBeInTheDocument();

    rerender(
      <BacklogDetail
        backlog={{ ...backlog, baseBranch: "release/2.4" }}
        project={project}
        tasks={[]}
      />,
    );
    expect(screen.getByText("release/2.4")).toBeInTheDocument();
  });

  it("omits the GitLab destination for a project with no linked GitLab project", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(
      screen.queryByText("GitLab project for new issues"),
    ).not.toBeInTheDocument();
  });

  it("shows the closed/total progress its timeline bar is filled by", () => {
    const tasks = [
      makeTask({ id: "t1", status: "closed" }),
      makeTask({ id: "t2", status: "closed" }),
      makeTask({ id: "t3", status: "open" }),
      makeTask({ id: "t4", status: "open" }),
    ];
    render(<BacklogDetail backlog={backlog} project={project} tasks={tasks} />);
    expect(screen.getByText("2/4 closed (50%)")).toBeInTheDocument();
  });

  // Progress is unknowable when the task fetch failed, so it must not read 0%.
  it("reports progress as unavailable when tasks failed to load", () => {
    render(
      <BacklogDetail
        backlog={backlog}
        project={project}
        tasks={[]}
        tasksError
      />,
    );
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });

  it("shows an empty state with no tasks", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(
      screen.getByText("No tasks in this backlog yet."),
    ).toBeInTheDocument();
  });

  it("lists the backlog's tasks", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Fix the bug" }),
      makeTask({ id: "t2", title: "Write docs" }),
    ];
    render(<BacklogDetail backlog={backlog} project={project} tasks={tasks} />);
    expect(screen.getByRole("link", { name: /Fix the bug/ })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t1",
    );
    expect(screen.getByRole("link", { name: /Write docs/ })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t2",
    );
  });

  it("shows a load error", () => {
    render(
      <BacklogDetail
        backlog={backlog}
        project={project}
        tasks={[]}
        tasksError
      />,
    );
    expect(
      screen.getByText("Failed to load tasks. Try refreshing the page."),
    ).toBeInTheDocument();
  });

  // The Backlog collection defaults to Board, where no row carries an Edit
  // control — the single view is where a backlog is edited (issue: editing a
  // backlog from the UI).
  it("edits the backlog in place and shows the saved values", async () => {
    const saved: Backlog = {
      ...backlog,
      name: "Sprint 2",
      baseBranch: "develop",
    };
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(saved), { status: 200 }),
    );

    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit backlog" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Sprint 2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Sprint 2" }),
      ).toBeInTheDocument(),
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/backlogs/b1",
      expect.objectContaining({ method: "PATCH" }),
    );
    // The breadcrumb outside this component names the backlog too, so the
    // page has to be re-fetched as well as the card updated in place.
    expect(refresh).toHaveBeenCalled();
    // The form is gone, and the saved value is read back from the response
    // rather than from what was typed.
    expect(screen.getByText("develop")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Save" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the backlog unchanged when the edit is cancelled", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit backlog" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.getByRole("heading", { name: "Sprint 1" }),
    ).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("deletes the backlog and returns to the collection", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));

    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    // The confirmation says where the backlog's tasks go, since they are not
    // deleted with it.
    expect(
      screen.getByText(
        "Its tasks will move to Unclassified. Delete this backlog?",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));

    await waitFor(() =>
      expect(push).toHaveBeenCalledWith("/projects/p1/backlogs"),
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/backlogs/b1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("does not delete until the confirmation is confirmed", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });
});
