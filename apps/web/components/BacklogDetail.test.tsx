import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Backlog, LinkedGitlabProject, Task } from "@/types";
import { BacklogDetail } from "./BacklogDetail";

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
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
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

describe("BacklogDetail", () => {
  it("shows identity and attributes", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(screen.getByRole("heading", { name: "Sprint 1" })).toBeInTheDocument();
    expect(screen.getByText("The first sprint")).toBeInTheDocument();
  });

  it("shows the planned period, or says it is unset", () => {
    const { rerender } = render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    // Start date, due date, and base branch each say "Not set" when unset.
    expect(screen.getAllByText("Not set")).toHaveLength(3);

    rerender(
      <BacklogDetail
        backlog={{ ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" }}
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
      <BacklogDetail backlog={backlog} project={project} tasks={[]} links={links} />,
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
    const { rerender } = render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
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
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} tasksError />);
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });

  it("shows an empty state with no tasks", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} />);
    expect(screen.getByText("No tasks in this backlog yet.")).toBeInTheDocument();
  });

  it("lists the backlog's tasks", () => {
    const tasks = [makeTask({ id: "t1", title: "Fix the bug" }), makeTask({ id: "t2", title: "Write docs" })];
    render(<BacklogDetail backlog={backlog} project={project} tasks={tasks} />);
    expect(screen.getByRole("link", { name: /Fix the bug/ })).toHaveAttribute("href", "/projects/p1/tasks/t1");
    expect(screen.getByRole("link", { name: /Write docs/ })).toHaveAttribute("href", "/projects/p1/tasks/t2");
  });

  it("shows a load error", () => {
    render(<BacklogDetail backlog={backlog} project={project} tasks={[]} tasksError />);
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });
});
