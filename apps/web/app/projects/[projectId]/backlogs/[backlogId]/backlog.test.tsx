import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Backlog, Project, Task, User } from "@/types";

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

const project: Project = {
  id: "p1",
  name: "Alpha",
  description: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  failedSyncTaskCount: 0,
};

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

const getCurrentUser = vi.fn();
const getBacklog = vi.fn();
const getProject = vi.fn();
const getTasks = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getBacklog: (id: string) => getBacklog(id),
  getProject: (id: string) => getProject(id),
  getTasks: (id: string) => getTasks(id),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import BacklogPage from "./page";

describe("BacklogPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getBacklog.mockResolvedValue(backlog);
    getProject.mockResolvedValue(project);
    getTasks.mockResolvedValue([]);
  });

  it("renders the backlog's single view, scoped to its own tasks", async () => {
    getTasks.mockResolvedValue([
      makeTask({ id: "t1", title: "In this backlog", backlogId: "b1" }),
      makeTask({ id: "t2", title: "In another backlog", backlogId: "b2" }),
    ]);
    render(await BacklogPage({ params: Promise.resolve({ projectId: "p1", backlogId: "b1" }) }));
    expect(screen.getByRole("heading", { name: "Sprint 1" })).toBeInTheDocument();
    expect(screen.getByText("In this backlog")).toBeInTheDocument();
    expect(screen.queryByText("In another backlog")).not.toBeInTheDocument();
  });

  // The project itself is reached from the sidebar in the surrounding layout,
  // so the breadcrumb here only has to climb to the collection.
  it("links back to the backlog collection", async () => {
    render(await BacklogPage({ params: Promise.resolve({ projectId: "p1", backlogId: "b1" }) }));
    expect(screen.getByRole("link", { name: "Backlogs" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs",
    );
  });

  it("hands its tasks off to the task collection, filtered to this backlog", async () => {
    render(await BacklogPage({ params: Promise.resolve({ projectId: "p1", backlogId: "b1" }) }));
    expect(screen.getByRole("link", { name: "Open in Tasks" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks?backlog=b1",
    );
  });

  it("renders not-found when the backlog doesn't exist", async () => {
    getBacklog.mockResolvedValue(null);
    await expect(
      BacklogPage({ params: Promise.resolve({ projectId: "p1", backlogId: "unknown" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });

  it("renders not-found when the backlog belongs to another project", async () => {
    await expect(
      BacklogPage({ params: Promise.resolve({ projectId: "p2", backlogId: "b1" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });

  it("renders not-found when the parent project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(BacklogPage({ params: Promise.resolve({ projectId: "p1", backlogId: "b1" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });

  it("shows a load error without failing the whole page when tasks fail to load", async () => {
    getTasks.mockRejectedValue(new Error("boom"));
    render(await BacklogPage({ params: Promise.resolve({ projectId: "p1", backlogId: "b1" }) }));
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });
});
