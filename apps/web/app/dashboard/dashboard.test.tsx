import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Project, TaskWithProject, User } from "@/types";

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

function makeTask(overrides: Partial<TaskWithProject>): TaskWithProject {
  return {
    id: "t1",
    projectId: "p1",
    projectName: "Alpha",
    backlogId: null,
    epicId: null,
    title: "Task",
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

const getCurrentUser = vi.fn();
const getProjects = vi.fn();
const getAllTasks = vi.fn();
const getFailedSyncProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProjects: () => getProjects(),
  getAllTasks: (...args: unknown[]) => getAllTasks(...args),
  getFailedSyncProjects: () => getFailedSyncProjects(),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
}));

import DashboardPage from "./page";
import { taskPage } from "@/lib/test-pages";

describe("DashboardPage", () => {
  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(DashboardPage()).rejects.toThrow("REDIRECT:/login");
  });

  it("shows a create-project prompt when the user has no projects", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProjects.mockResolvedValue([]);

    render(await DashboardPage());

    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
    expect(screen.getByText("No projects yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create a project" })).toHaveAttribute(
      "href",
      "/projects",
    );
    expect(getAllTasks).not.toHaveBeenCalled();
  });

  it("renders every section for a user with projects and tasks", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProjects.mockResolvedValue([project]);
    getAllTasks
      .mockResolvedValueOnce(
        taskPage([makeTask({ id: "t1", title: "Overdue task", dueOn: "2020-01-01" })]),
      )
      .mockResolvedValueOnce(taskPage([]))
      .mockResolvedValueOnce(taskPage([]))
      .mockResolvedValueOnce(taskPage([]));
    getFailedSyncProjects.mockResolvedValue([{ ...project, failedSyncTaskCount: 2 }]);

    render(await DashboardPage());

    expect(screen.getByText("Overdue task")).toBeInTheDocument();
    expect(screen.getByText(/Sync failures/)).toBeInTheDocument();
    expect(screen.getByText("2 failed")).toBeInTheDocument();
  });

  it("shows an error state when the task/project fetches fail", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProjects.mockResolvedValue([project]);
    getAllTasks.mockRejectedValue(new Error("boom"));
    getFailedSyncProjects.mockResolvedValue([]);

    render(await DashboardPage());

    expect(screen.getByText(/Failed to load the dashboard/)).toBeInTheDocument();
  });
});
