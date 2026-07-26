import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Project, User } from "@/types";

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

const getCurrentUser = vi.fn();
const getProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProjects: () => getProjects(),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
}));

import ProjectsPage from "./page";

const project: Project = {
  id: "1",
  name: "Alpha",
  description: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("ProjectsPage", () => {
  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(ProjectsPage()).rejects.toThrow("REDIRECT:/login");
  });

  it("renders the projects returned by the API", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProjects.mockResolvedValue([project]);
    render(await ProjectsPage());
    expect(screen.getByText("Alpha")).toBeInTheDocument();
  });

  it("renders the error state when the project fetch fails", async () => {
    getCurrentUser.mockResolvedValue(user);
    getProjects.mockRejectedValue(new Error("Failed to load projects: 500"));
    render(await ProjectsPage());
    expect(screen.getByText(/Failed to load projects/i)).toBeInTheDocument();
  });
});
