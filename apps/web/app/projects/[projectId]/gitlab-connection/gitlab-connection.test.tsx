import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { GitlabConnection, LinkedGitlabProject, Project, User } from "@/types";

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
  tokenLastFour: "a1b2",
  tokenGitlabUserId: 42,
  tokenGitlabUsername: "octocat",
  verified: true,
  lastVerifiedAt: "2026-01-05T09:00:00Z",
  lastVerifyError: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-05T09:00:00Z",
};

const link: LinkedGitlabProject = {
  id: "l1",
  gitlabConnectionId: "c1",
  gitlabProjectId: 100,
  pathWithNamespace: "team/demo",
  name: "demo",
  webUrl: "https://gitlab.example.com/team/demo",
  syncScope: "all",
  syncLabels: [],
  isDefault: true,
  initialImportStatus: "completed",
  lastSyncedAt: "2026-01-06T12:00:00Z",
  webhookStatus: "registered",
  webhookRegisteredAt: "2026-01-05T09:05:00Z",
  webhookError: "",
  createdAt: "2026-01-05T09:00:00Z",
  updatedAt: "2026-01-06T12:00:00Z",
};

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getGitlabConnection = vi.fn();
const getLinkedGitlabProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getGitlabConnection: (id: string) => getGitlabConnection(id),
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
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

import GitlabConnectionPage from "./page";

describe("GitlabConnectionPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getGitlabConnection.mockResolvedValue(connection);
    getLinkedGitlabProjects.mockResolvedValue([link]);
  });

  it("shows the connection and links every linked project to its single view", async () => {
    render(await GitlabConnectionPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("https://gitlab.example.com")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /team\/demo/ })).toHaveAttribute(
      "href",
      "/projects/p1/linked-gitlab-projects/l1",
    );
  });

  it("offers the connect form and hides linking when no connection exists", async () => {
    getGitlabConnection.mockResolvedValue(null);
    getLinkedGitlabProjects.mockResolvedValue([]);
    render(await GitlabConnectionPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("form", { name: "Connect GitLab" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Link a project" })).not.toBeInTheDocument();
  });

  it("renders not-found when the project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(
      GitlabConnectionPage({ params: Promise.resolve({ projectId: "unknown" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });
});
