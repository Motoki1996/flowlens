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
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const task: Task = {
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
};

const getCurrentUser = vi.fn();
const getTask = vi.fn();
const getProject = vi.fn();
const getBacklogs = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getTask: (id: string) => getTask(id),
  getProject: (id: string) => getProject(id),
  getBacklogs: (id: string) => getBacklogs(id),
  getTasks: () => Promise.resolve([]),
  getTaskDependencies: () => Promise.resolve([]),
  // The single view loads the project's default linked GitLab project to fill
  // the assignee/label pickers (issue #80); with none linked it falls back to
  // free-text entry, which is what these cases exercise.
  getLinkedGitlabProjects: () => Promise.resolve([]),
  getLinkedGitlabProjectMembers: () => Promise.resolve([]),
  getLinkedGitlabProjectLabels: () => Promise.resolve([]),
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

import TaskPage from "./page";

describe("TaskPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getTask.mockResolvedValue(task);
    getProject.mockResolvedValue(project);
    getBacklogs.mockResolvedValue([backlog]);
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(TaskPage({ params: Promise.resolve({ projectId: "p1", taskId: "t1" }) })).rejects.toThrow(
      "REDIRECT:/login",
    );
  });

  it("renders the task's single view with its backlog resolved by name", async () => {
    render(await TaskPage({ params: Promise.resolve({ projectId: "p1", taskId: "t1" }) }));
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Sprint 1");
    expect(getTask).toHaveBeenCalledWith("t1");
  });

  // The project itself is reached from the sidebar in the surrounding layout,
  // so the breadcrumb here only has to climb to the collection.
  it("links back to the task collection", async () => {
    render(await TaskPage({ params: Promise.resolve({ projectId: "p1", taskId: "t1" }) }));
    expect(screen.getByRole("link", { name: "Tasks" })).toHaveAttribute("href", "/projects/p1/tasks");
  });

  it("renders not-found when the task doesn't exist", async () => {
    getTask.mockResolvedValue(null);
    await expect(
      TaskPage({ params: Promise.resolve({ projectId: "p1", taskId: "unknown" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });

  it("renders not-found when the task belongs to another project", async () => {
    await expect(
      TaskPage({ params: Promise.resolve({ projectId: "p2", taskId: "t1" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });
});
