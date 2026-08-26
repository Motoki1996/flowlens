import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Backlog, Epic, Project, User } from "@/types";

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
  startDate: null,
  dueOn: null,
  priority: "medium",
  progress: "not_started",
  defaultLinkedGitlabProjectId: null,
  baseBranch: "main",
  allowedScope: "",
  forbiddenScope: "",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  taskCount: 0,
  closedTaskCount: 0,
  status: "open",
  closedAt: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const epic: Epic = {
  id: "e1",
  projectId: "p1",
  backlogId: "b1",
  name: "Screens",
  description: "",
  startDate: null,
  dueOn: null,
  priority: "medium",
  progress: "not_started",
  defaultLinkedGitlabProjectId: null,
  baseBranch: "",
  allowedScope: "",
  forbiddenScope: "",
  estimatedPoints: null,
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  taskCount: 0,
  closedTaskCount: 0,
  status: "open",
  closedAt: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getEpics = vi.fn();
const getBacklogs = vi.fn();
const getTasks = vi.fn();
const getLinkedGitlabProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getEpics: (id: string, filter?: unknown) => getEpics(id, filter),
  getBacklogs: (id: string) => getBacklogs(id),
  getTasks: (id: string, filter?: unknown) => getTasks(id, filter),
  MAX_TASKS_PER_PAGE: 200,
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/p1/epics",
  useSearchParams: () => new URLSearchParams(),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import EpicsPage from "./page";
import { taskPage } from "@/lib/test-pages";

describe("EpicsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getEpics.mockResolvedValue([epic]);
    getBacklogs.mockResolvedValue([backlog]);
    getTasks.mockResolvedValue(taskPage([]));
    getLinkedGitlabProjects.mockResolvedValue([]);
  });

  it("renders the epic collection", async () => {
    render(await EpicsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("link", { name: /Screens/ })).toHaveAttribute(
      "href",
      "/projects/p1/epics/e1",
    );
  });

  it("renders not-found when the project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(EpicsPage({ params: Promise.resolve({ projectId: "unknown" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });

  // The same in-page catch the Backlog collection has: one failed fetch must
  // not take "New epic" and the rest of the screen with it.
  it("shows a load error without failing the whole page when epics fail to load", async () => {
    getEpics.mockRejectedValue(new Error("boom"));
    render(await EpicsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText(/Failed to load epics/)).toBeInTheDocument();
  });

  // The three supporting fetches are caught individually and degrade to an
  // empty list, because "no backlogs to file this under" is a real state the
  // form already handles — unlike the epics themselves, which have a message.
  it("still renders when the supporting fetches fail", async () => {
    getBacklogs.mockRejectedValue(new Error("boom"));
    getTasks.mockRejectedValue(new Error("boom"));
    getLinkedGitlabProjects.mockRejectedValue(new Error("boom"));
    render(await EpicsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("link", { name: /Screens/ })).toBeInTheDocument();
    expect(screen.queryByText(/Failed to load epics/)).not.toBeInTheDocument();
  });

  // ?priority=/?progress=/?sort= reach the API; ?backlog= is translated from
  // the URL's own word for "filed nowhere" into the API's.
  it("forwards the filters to getEpics", async () => {
    await EpicsPage({
      params: Promise.resolve({ projectId: "p1" }),
      searchParams: Promise.resolve({ priority: "urgent", progress: "on_hold", sort: "priority" }),
    });
    expect(getEpics).toHaveBeenCalledWith("p1", {
      status: "open",
      backlogId: undefined,
      priority: "urgent",
      progress: "on_hold",
      sort: "priority",
    });
  });

  it("translates ?backlog=none into the API's 'unassigned'", async () => {
    await EpicsPage({
      params: Promise.resolve({ projectId: "p1" }),
      searchParams: Promise.resolve({ backlog: "none" }),
    });
    expect(getEpics).toHaveBeenCalledWith(
      "p1",
      expect.objectContaining({ backlogId: "unassigned" }),
    );
  });

  // These come straight off a hand-editable query string, so a junk value has
  // to fall back to the default rather than reach the API or throw.
  it("drops unrecognised filter values instead of forwarding them", async () => {
    await EpicsPage({
      params: Promise.resolve({ projectId: "p1" }),
      searchParams: Promise.resolve({ priority: "critical", progress: "wat", sort: "nonsense" }),
    });
    expect(getEpics).toHaveBeenCalledWith("p1", {
      status: "open",
      backlogId: undefined,
      priority: undefined,
      progress: undefined,
      sort: undefined,
    });
  });

  // "dueOn" is a recognised URL value the API has no counterpart for — it is
  // applied client-side, so it must not be forwarded as a sort.
  it("keeps the client-side dueOn sort out of the API call", async () => {
    await EpicsPage({
      params: Promise.resolve({ projectId: "p1" }),
      searchParams: Promise.resolve({ sort: "dueOn" }),
    });
    expect(getEpics).toHaveBeenCalledWith("p1", expect.objectContaining({ sort: undefined }));
  });

  it("opens in the view mode ?view= names, and in Board for anything else", async () => {
    render(
      await EpicsPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ view: "list" }),
      }),
    );
    expect(screen.getByRole("button", { name: "List" })).toHaveAttribute("aria-pressed", "true");

    render(
      await EpicsPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ view: "nonsense" }),
      }),
    );
    expect(screen.getAllByRole("button", { name: "Board" })[1]).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });
});
