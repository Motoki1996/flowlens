import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Project, User } from "@/types";

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getTasks = vi.fn();
const getBacklogs = vi.fn();
const getEpics = vi.fn();
const getGitlabConnection = vi.fn();
const getLinkedGitlabProjects = vi.fn();
const getFailedSyncJobs = vi.fn();
const getProjectMetrics = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getTasks: (id: string) => getTasks(id),
  getBacklogs: (id: string) => getBacklogs(id),
  getEpics: (id: string) => getEpics(id),
  getGitlabConnection: (id: string) => getGitlabConnection(id),
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
  getFailedSyncJobs: (id: string) => getFailedSyncJobs(id),
  getProjectMetrics: (id: string) => getProjectMetrics(id),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/1",
  useSearchParams: () => new URLSearchParams(),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import ProjectPage from "./page";

const project: Project = {
  id: "1",
  name: "Alpha",
  description: "The first project",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  failedSyncTaskCount: 0,
};

describe("ProjectPage", () => {
  beforeEach(() => {
    getTasks.mockResolvedValue([]);
    getBacklogs.mockResolvedValue([]);
    getEpics.mockResolvedValue([]);
    getGitlabConnection.mockResolvedValue(null);
    getLinkedGitlabProjects.mockResolvedValue([]);
    getFailedSyncJobs.mockResolvedValue([]);
    getProjectMetrics.mockResolvedValue(null);
  });

  it("renders the project's single view", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    render(await ProjectPage({ params: Promise.resolve({ projectId: "1" }) }));
    expect(screen.getByRole("heading", { name: "Alpha" })).toBeInTheDocument();
    expect(getProject).toHaveBeenCalledWith("1");
    expect(getTasks).toHaveBeenCalledWith("1");
    expect(getBacklogs).toHaveBeenCalledWith("1");
    expect(getFailedSyncJobs).toHaveBeenCalledWith("1");
  });

  it("shows the failed sync jobs section with a retry affordance", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getFailedSyncJobs.mockResolvedValue([
      {
        id: "j1",
        projectId: "1",
        kind: "issue.update",
        status: "failed",
        attempts: 5,
        lastError: "GitLab returned 500",
        runAfter: "2026-01-01T00:00:00Z",
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      },
    ]);
    render(await ProjectPage({ params: Promise.resolve({ projectId: "1" }) }));
    expect(screen.getByText("Failed sync jobs")).toBeInTheDocument();
    expect(screen.getByText("GitLab returned 500")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("summarises the task and backlog collections rather than listing them", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getBacklogs.mockResolvedValue([{ id: "b1" }, { id: "b2" }]);
    getTasks.mockResolvedValue([
      { id: "t1", title: "Fix the bug", status: "open" },
      { id: "t2", title: "Write docs", status: "closed" },
    ]);
    getEpics.mockResolvedValue([{ id: "e1" }]);
    render(await ProjectPage({ params: Promise.resolve({ projectId: "1" }) }));
    expect(screen.getByText("2 backlogs")).toBeInTheDocument();
    expect(screen.getByText("1 epics")).toBeInTheDocument();
    expect(screen.getByText("1 open / 2 total")).toBeInTheDocument();
    expect(screen.queryByText("Fix the bug")).not.toBeInTheDocument();
  });

  it("renders not-found when the project doesn't exist", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(null);
    await expect(ProjectPage({ params: Promise.resolve({ projectId: "unknown" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });

  it("keeps the collection links usable when their counts fail to load", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getTasks.mockRejectedValue(new Error("boom"));
    render(await ProjectPage({ params: Promise.resolve({ projectId: "1" }) }));
    // All three counts come from the one Promise.all, so one failure takes
    // every count with it — and every link still has to work.
    expect(screen.getAllByText("Count unavailable")).toHaveLength(3);
    expect(screen.getByRole("link", { name: /Tasks/ })).toHaveAttribute("href", "/projects/1/tasks");
    expect(screen.getByRole("link", { name: /Epics/ })).toHaveAttribute("href", "/projects/1/epics");
  });
});
