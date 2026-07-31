import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { LinkedGitlabProject, Project, SyncRun, User } from "@/types";

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

const run: SyncRun = {
  id: "run-1",
  linkedGitlabProjectId: "l1",
  kind: "manual_resync",
  status: "succeeded",
  issuesSeen: 5,
  issuesCreated: 3,
  issuesUpdated: 2,
  startedAt: "2026-01-06T12:00:00Z",
  completedAt: "2026-01-06T12:01:00Z",
  errorMessage: "",
  createdAt: "2026-01-06T12:00:00Z",
};

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getLinkedGitlabProjects = vi.fn();
const getSyncRuns = vi.fn();
const getWebhookEvents = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
  getSyncRuns: (id: string) => getSyncRuns(id),
  getWebhookEvents: (id: string) => getWebhookEvents(id),
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

import LinkedGitlabProjectPage from "./page";

const params = Promise.resolve({ projectId: "p1", linkId: "l1" });

describe("LinkedGitlabProjectPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getLinkedGitlabProjects.mockResolvedValue([link]);
    getSyncRuns.mockResolvedValue([run]);
    getWebhookEvents.mockResolvedValue([]);
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(LinkedGitlabProjectPage({ params })).rejects.toThrow("REDIRECT:/login");
  });

  it("renders the link with its sync history, under a breadcrumb to the connection", async () => {
    render(await LinkedGitlabProjectPage({ params }));
    expect(screen.getByRole("heading", { name: "team/demo" })).toBeInTheDocument();
    expect(screen.getByText("5 seen, 3 created, 2 updated")).toBeInTheDocument();
    const breadcrumb = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(breadcrumb.getByRole("link", { name: "GitLab connection" })).toHaveAttribute(
      "href",
      "/projects/p1/gitlab-connection",
    );
  });

  it("renders not-found when the link doesn't belong to the project", async () => {
    getLinkedGitlabProjects.mockResolvedValue([]);
    await expect(LinkedGitlabProjectPage({ params })).rejects.toThrow("NOT_FOUND");
  });

  it("still renders when the sync history fails to load", async () => {
    getSyncRuns.mockRejectedValue(new Error("boom"));
    render(await LinkedGitlabProjectPage({ params }));
    expect(screen.getByText("No sync runs yet.")).toBeInTheDocument();
  });
});
