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
  getTasks: (id: string, filter?: unknown) => getTasks(id, filter),
  getBacklogs: (id: string) => getBacklogs(id),
  getTaskDependencies: (id: string) => getTaskDependencies(id),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/p1/tasks",
  useSearchParams: () => new URLSearchParams(),
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

  it("renders the task collection, asking the API for open tasks by default", async () => {
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("Tasks")).toBeInTheDocument();
    // Only status is sent: every other filter is off, and the absent sort is
    // the API's own manual/position order.
    expect(getTasks).toHaveBeenCalledWith("p1", {
      backlogId: undefined,
      status: "open",
      progress: undefined,
      priority: undefined,
      sort: undefined,
      q: undefined,
    });
    expect(getBacklogs).toHaveBeenCalledWith("p1");
  });

  it("passes the URL query to the API as the request's filter (issue #143)", async () => {
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
        progress: "in_progress",
      },
    ]);
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({
          backlog: "8f0f2c1e-2c9c-4b1f-9f4a-0d5b1f6c7a21",
          status: "all",
          progress: "in_progress",
          priority: "high",
          sort: "priority",
          q: "sprint",
        }),
      }),
    );

    expect(getTasks).toHaveBeenCalledWith("p1", {
      backlogId: "8f0f2c1e-2c9c-4b1f-9f4a-0d5b1f6c7a21",
      status: undefined,
      progress: "in_progress",
      priority: "high",
      sort: "priority",
      q: "sprint",
    });
    expect(screen.getByText("In sprint 1")).toBeInTheDocument();
  });

  it("sends the Unclassified group as the API's backlog_id=unassigned", async () => {
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ backlog: "unclassified" }),
      }),
    );
    expect(getTasks).toHaveBeenCalledWith("p1", expect.objectContaining({ backlogId: "unassigned" }));
  });

  it("falls back to the defaults for query values the API would reject", async () => {
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({
          backlog: "not-a-uuid",
          status: "banana",
          priority: "extreme",
          sort: "sideways",
        }),
      }),
    );
    expect(getTasks).toHaveBeenCalledWith("p1", {
      backlogId: undefined,
      status: "open",
      progress: undefined,
      priority: undefined,
      sort: undefined,
      q: undefined,
    });
  });

  it("pre-selects the backlog filter from ?backlog=", async () => {
    const backlogId = "8f0f2c1e-2c9c-4b1f-9f4a-0d5b1f6c7a21";
    getBacklogs.mockResolvedValue([
      { id: backlogId, projectId: "p1", name: "Sprint 1", description: "", position: 0 },
    ]);
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ backlog: backlogId }),
      }),
    );
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Sprint 1");
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
