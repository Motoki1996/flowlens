import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Project, User } from "@/types";

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

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getTasks = vi.fn();
const getBacklogs = vi.fn();
const getTaskDependencies = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getTasks: (id: string) => getTasks(id),
  getBacklogs: (id: string) => getBacklogs(id),
  getTaskDependencies: (id: string) => getTaskDependencies(id),
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

import TasksPage from "./page";

describe("TasksPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getTasks.mockResolvedValue([]);
    getBacklogs.mockResolvedValue([]);
    getTaskDependencies.mockResolvedValue([]);
  });

  it("renders the task collection", async () => {
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("Tasks")).toBeInTheDocument();
    expect(getTasks).toHaveBeenCalledWith("p1");
    expect(getBacklogs).toHaveBeenCalledWith("p1");
  });

  it("pre-selects the backlog filter from ?backlog=", async () => {
    getBacklogs.mockResolvedValue([
      { id: "b1", projectId: "p1", name: "Sprint 1", description: "", position: 0 },
    ]);
    getTasks.mockResolvedValue([
      {
        id: "t1",
        projectId: "p1",
        backlogId: "b1",
        title: "In sprint 1",
        status: "open",
        labels: [],
        priority: "medium",
        progress: "not_started",
      },
      {
        id: "t2",
        projectId: "p1",
        backlogId: null,
        title: "Loose end",
        status: "open",
        labels: [],
        priority: "medium",
        progress: "not_started",
      },
    ]);
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ backlog: "b1" }),
      }),
    );
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Sprint 1");
    expect(screen.getByText("In sprint 1")).toBeInTheDocument();
    expect(screen.queryByText("Loose end")).not.toBeInTheDocument();
  });

  it("renders not-found when the project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(TasksPage({ params: Promise.resolve({ projectId: "unknown" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });

  it("shows a load error without failing the whole page when tasks fail to load", async () => {
    getTasks.mockRejectedValue(new Error("boom"));
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });
});
