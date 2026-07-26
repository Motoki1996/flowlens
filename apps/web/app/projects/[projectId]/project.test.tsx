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

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getTasks: (id: string) => getTasks(id),
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

import ProjectPage from "./page";

const project: Project = {
  id: "1",
  name: "Alpha",
  description: "The first project",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("ProjectPage", () => {
  beforeEach(() => {
    getTasks.mockResolvedValue([]);
    getBacklogs.mockResolvedValue([]);
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(ProjectPage({ params: Promise.resolve({ projectId: "1" }) })).rejects.toThrow(
      "REDIRECT:/login",
    );
  });

  it("renders the project's single view", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    render(await ProjectPage({ params: Promise.resolve({ projectId: "1" }) }));
    expect(screen.getByRole("heading", { name: "Alpha" })).toBeInTheDocument();
    expect(getProject).toHaveBeenCalledWith("1");
    expect(getTasks).toHaveBeenCalledWith("1");
    expect(getBacklogs).toHaveBeenCalledWith("1");
  });

  it("renders not-found when the project doesn't exist", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(null);
    await expect(ProjectPage({ params: Promise.resolve({ projectId: "unknown" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });

  it("shows a load error without failing the whole page when tasks fail to load", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getTasks.mockRejectedValue(new Error("boom"));
    render(await ProjectPage({ params: Promise.resolve({ projectId: "1" }) }));
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });
});
