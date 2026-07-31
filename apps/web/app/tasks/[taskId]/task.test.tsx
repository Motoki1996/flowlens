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
    getBacklogs.mockResolvedValue([backlog]);
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(TaskPage({ params: Promise.resolve({ taskId: "t1" }) })).rejects.toThrow(
      "REDIRECT:/login",
    );
  });

  it("renders the task's single view with its backlog resolved by name", async () => {
    getCurrentUser.mockResolvedValue(user);
    getTask.mockResolvedValue(task);
    getProject.mockResolvedValue(project);
    render(await TaskPage({ params: Promise.resolve({ taskId: "t1" }) }));
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveValue("b1");
    expect(getTask).toHaveBeenCalledWith("t1");
  });

  it("renders not-found when the task doesn't exist", async () => {
    getCurrentUser.mockResolvedValue(user);
    getTask.mockResolvedValue(null);
    await expect(TaskPage({ params: Promise.resolve({ taskId: "unknown" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });
});
