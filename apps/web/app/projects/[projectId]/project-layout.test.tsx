import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
const getEpics = vi.fn();
const getTasks = vi.fn();
const getGitlabConnection = vi.fn();
const getLinkedGitlabProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getProjects: () => getProjects(),
  getBacklogs: (id: string) => getBacklogs(id),
  getEpics: (id: string) => getEpics(id),
  getTasks: (id: string, filter?: unknown) => getTasks(id, filter),
  getGitlabConnection: (id: string) => getGitlabConnection(id),
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
}));
// The layout reads the sidebar's remembered geometry from the request's
// cookies; tests set them through this map.
const cookieJar = new Map<string, string>();

vi.mock("next/headers", () => ({
  cookies: async () => ({
    get: (name: string) => {
      const value = cookieJar.get(name);
      return value === undefined ? undefined : { name, value };
    },
  }),
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
import { taskPage } from "@/lib/test-pages";

function renderLayout() {
  return ProjectLayout({
    children: <p>screen content</p>,
    params: Promise.resolve({ projectId: "p1" }),
  });
}

describe("ProjectLayout", () => {
  beforeEach(() => {
    cookieJar.clear();
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getProjects.mockResolvedValue([project]);
    getBacklogs.mockResolvedValue([{ id: "b1" }, { id: "b2" }]);
    getEpics.mockResolvedValue([{ id: "e1" }]);
    getTasks.mockResolvedValue(taskPage([
      { id: "t1", status: "open" },
      { id: "t2", status: "closed" },
    ]));
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
    expect(nav.getByRole("link", { name: /^Epics/ })).toHaveTextContent("1");
    expect(nav.getByRole("link", { name: /^Tasks/ })).toHaveTextContent("1/2");
    expect(nav.getByRole("link", { name: /^GitLab connection/ })).toHaveTextContent("1");
  });

  it("reports a broken GitLab connection in the sidebar", async () => {
    getGitlabConnection.mockResolvedValue({ ...connection, lastVerifyError: "401 Unauthorized" });
    render(await renderLayout());
    const nav = within(screen.getByRole("navigation", { name: "Project sections" }));
    expect(nav.getByRole("link", { name: /^GitLab connection/ })).toHaveTextContent("Error");
  });

  // The sidebar's geometry is server-rendered from the cookie rather than
  // restored on the client, so the first paint is already the right shape.
  it("opens the sidebar at its default width when nothing was remembered", async () => {
    const { container } = render(await renderLayout());
    expect(container.querySelector('[data-slot="sidebar-wrapper"]')).toHaveStyle({
      "--sidebar-width": "240px",
    });
    expect(container.querySelector('[data-slot="sidebar"][data-state]')).toHaveAttribute(
      "data-state",
      "expanded",
    );
  });

  it("restores the sidebar's collapsed state and width from the request's cookies", async () => {
    cookieJar.set("sidebar_state", "false");
    cookieJar.set("flowlens_sidebar_width", "320");
    const { container } = render(await renderLayout());
    expect(container.querySelector('[data-slot="sidebar-wrapper"]')).toHaveStyle({
      "--sidebar-width": "320px",
    });
    expect(container.querySelector('[data-slot="sidebar"][data-state]')).toHaveAttribute(
      "data-state",
      "collapsed",
    );
  });

  it("clamps a width cookie that is out of range", async () => {
    cookieJar.set("flowlens_sidebar_width", "9999");
    const { container } = render(await renderLayout());
    expect(container.querySelector('[data-slot="sidebar-wrapper"]')).toHaveStyle({
      "--sidebar-width": "480px",
    });
  });

  it("collapses the sidebar from its own toggle, and remembers it", async () => {
    const { container } = render(await renderLayout());
    const sidebar = container.querySelector('[data-slot="sidebar"]') as HTMLElement;
    await userEvent.click(within(sidebar).getByRole("button", { name: "Collapse sidebar" }));
    expect(container.querySelector('[data-slot="sidebar"][data-state]')).toHaveAttribute(
      "data-state",
      "collapsed",
    );
    expect(document.cookie).toContain("sidebar_state=false");
  });

  it("keeps the header's toggle for mobile only, where the drawer hides the sidebar's own", async () => {
    const { container } = render(await renderLayout());
    const header = container.querySelector("header") as HTMLElement;
    expect(within(header).getByRole("button", { name: "Collapse sidebar" })).toHaveClass(
      "md:hidden",
    );
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
