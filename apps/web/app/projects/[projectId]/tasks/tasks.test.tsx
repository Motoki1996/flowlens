import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
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

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(TasksPage({ params: Promise.resolve({ projectId: "p1" }) })).rejects.toThrow(
      "REDIRECT:/login",
    );
  });

  it("renders the task collection under a breadcrumb back to the project", async () => {
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    const breadcrumb = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(breadcrumb.getByRole("link", { name: "Alpha" })).toHaveAttribute("href", "/projects/p1");
    expect(breadcrumb.getByText("Tasks")).toBeInTheDocument();
    expect(getTasks).toHaveBeenCalledWith("p1");
    expect(getBacklogs).toHaveBeenCalledWith("p1");
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
