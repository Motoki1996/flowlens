import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { Backlog, Project, User } from "@/types";

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

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getBacklogs = vi.fn();
const getTasks = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getBacklogs: (id: string) => getBacklogs(id),
  getTasks: (id: string) => getTasks(id),
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

import BacklogsPage from "./page";

describe("BacklogsPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getBacklogs.mockResolvedValue([backlog]);
    getTasks.mockResolvedValue([]);
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) })).rejects.toThrow(
      "REDIRECT:/login",
    );
  });

  it("renders the backlog collection", async () => {
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("link", { name: /Sprint 1/ })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b1",
    );
  });

  // Task counts and timeline progress both come from tasks, so a failed fetch
  // has to reach the collection rather than reading as "no tasks".
  it("reports a failed task fetch instead of showing every backlog as empty", async () => {
    getBacklogs.mockResolvedValue([
      { ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" },
    ]);
    getTasks.mockRejectedValue(new Error("boom"));
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));
    // The timeline is loaded on demand, so its message only appears once that
    // chunk resolves.
    expect(
      await screen.findByText("Failed to load tasks — progress is unavailable.", undefined, {
        timeout: 15000,
      }),
    ).toBeInTheDocument();
  });

  it("links each backlog to the task collection filtered to it", async () => {
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("link", { name: "View tasks" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks?backlog=b1",
    );
  });

  it("renders not-found when the project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(
      BacklogsPage({ params: Promise.resolve({ projectId: "unknown" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });

  it("falls back to zero task counts when tasks fail to load", async () => {
    getTasks.mockRejectedValue(new Error("boom"));
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("link", { name: /Sprint 1/ })).toHaveTextContent("(0)");
  });
});
