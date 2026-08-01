import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { GitlabConnection, Project, User } from "@/types";

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

const connection: GitlabConnection = {
  projectId: "p1",
  baseUrl: "https://gitlab.example.com",
  tokenLastFour: "abcd",
  tokenGitlabUserId: 1,
  tokenGitlabUsername: "octocat",
  verified: true,
  lastVerifiedAt: "2026-01-01T00:00:00Z",
  lastVerifyError: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getProjects = vi.fn();
const getBacklogs = vi.fn();
const getTasks = vi.fn();
const getGitlabConnection = vi.fn();
const getLinkedGitlabProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getProjects: () => getProjects(),
  getBacklogs: (id: string) => getBacklogs(id),
  getTasks: (id: string) => getTasks(id),
  getGitlabConnection: (id: string) => getGitlabConnection(id),
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/p1/tasks",
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import ProjectLayout from "./layout";

function renderLayout() {
  return ProjectLayout({
    children: <p>screen content</p>,
    params: Promise.resolve({ projectId: "p1" }),
  });
}

describe("ProjectLayout", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getProjects.mockResolvedValue([project]);
    getBacklogs.mockResolvedValue([{ id: "b1" }, { id: "b2" }]);
    getTasks.mockResolvedValue([
      { id: "t1", status: "open" },
      { id: "t2", status: "closed" },
    ]);
    getGitlabConnection.mockResolvedValue(connection);
    getLinkedGitlabProjects.mockResolvedValue([{ id: "l1" }]);
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(renderLayout()).rejects.toThrow("REDIRECT:/login");
  });

  it("renders not-found when the project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(renderLayout()).rejects.toThrow("NOT_FOUND");
  });

  it("wraps the screen in the project's sidebar with its collection counts", async () => {
    render(await renderLayout());
    expect(screen.getByText("screen content")).toBeInTheDocument();
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    expect(nav.getByRole("link", { name: /^Backlogs/ })).toHaveTextContent("2");
    expect(nav.getByRole("link", { name: /^Tasks/ })).toHaveTextContent("1/2");
    expect(nav.getByRole("link", { name: /^GitLab connection/ })).toHaveTextContent("1");
  });

  it("reports a broken GitLab connection in the sidebar", async () => {
    getGitlabConnection.mockResolvedValue({ ...connection, lastVerifyError: "401 Unauthorized" });
    render(await renderLayout());
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    expect(nav.getByRole("link", { name: /^GitLab connection/ })).toHaveTextContent("Error");
  });

  it("still renders the screen when the sidebar's counts fail to load", async () => {
    getTasks.mockRejectedValue(new Error("boom"));
    getGitlabConnection.mockRejectedValue(new Error("boom"));
    getProjects.mockRejectedValue(new Error("boom"));
    render(await renderLayout());
    expect(screen.getByText("screen content")).toBeInTheDocument();
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    expect(nav.getByRole("link", { name: "Tasks" })).toHaveAttribute("href", "/projects/p1/tasks");
  });
});
