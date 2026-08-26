import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import type { Backlog, Epic, Project, Task } from "@/types";

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
  allowedScope: "apps/**",
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

function makeEpic(overrides: Partial<Epic> = {}): Epic {
  return {
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
    ...overrides,
  };
}

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    epicId: "e1",
    title: "Build the list screen",
    description: "",
    status: "open",
    priority: "medium",
    progress: "not_started",
    size: "m",
    startDate: null,
    dueOn: null,
    labels: [],
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  } as Task;
}

const getProject = vi.fn();
const getEpic = vi.fn();
const getBacklogs = vi.fn();
const getTasks = vi.fn();
const getLinkedGitlabProjects = vi.fn();

vi.mock("@/lib/api", () => ({
  getProject: (id: string) => getProject(id),
  getEpic: (id: string) => getEpic(id),
  getBacklogs: (id: string) => getBacklogs(id),
  getTasks: (id: string) => getTasks(id),
  getLinkedGitlabProjects: (id: string) => getLinkedGitlabProjects(id),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/p1/epics/e1",
  useSearchParams: () => new URLSearchParams(),
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import EpicPage from "./page";

const params = Promise.resolve({ projectId: "p1", epicId: "e1" });

describe("EpicPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getProject.mockResolvedValue(project);
    getEpic.mockResolvedValue(makeEpic());
    getBacklogs.mockResolvedValue([backlog]);
    getTasks.mockResolvedValue([makeTask()]);
    getLinkedGitlabProjects.mockResolvedValue([]);
  });

  it("renders the epic's single view", async () => {
    render(await EpicPage({ params }));
    expect(screen.getByRole("heading", { name: "Screens" })).toBeInTheDocument();
  });

  // The epic's backlog is the rung above it, so the trail runs through the
  // backlog rather than straight up to the Epic collection.
  it("routes its breadcrumb trail through the parent backlog", async () => {
    render(await EpicPage({ params }));
    // Scoped to the trail: EpicDetail links the same backlog again in its
    // attribute list, which is a different statement about the same object.
    const trail = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(trail.getByRole("link", { name: "Sprint 1" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b1",
    );
    expect(trail.getByRole("link", { name: "Epics" })).toHaveAttribute("href", "/projects/p1/epics");
  });

  it("hangs an unfiled epic off the Epic collection directly", async () => {
    getEpic.mockResolvedValue(makeEpic({ backlogId: null }));
    render(await EpicPage({ params }));
    const trail = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(trail.queryByRole("link", { name: "Sprint 1" })).not.toBeInTheDocument();
    expect(trail.getByRole("link", { name: "Epics" })).toBeInTheDocument();
  });

  it("renders not-found when the epic doesn't exist", async () => {
    getEpic.mockResolvedValue(null);
    await expect(EpicPage({ params })).rejects.toThrow("NOT_FOUND");
  });

  // The cross-project guard: an epic reached through the wrong project's URL
  // is not that project's epic, so the nested route treats it as missing
  // rather than rendering someone else's object under this project's chrome.
  it("renders not-found when the epic belongs to another project", async () => {
    getEpic.mockResolvedValue(makeEpic({ projectId: "other" }));
    await expect(EpicPage({ params })).rejects.toThrow("NOT_FOUND");
  });

  it("renders not-found when the parent project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(EpicPage({ params })).rejects.toThrow("NOT_FOUND");
  });

  // The page fetches the whole project's tasks — the picker needs the
  // backlog's free ones — and narrows to the epic's own for the Tasks card.
  it("shows only the epic's own tasks while fetching the project's", async () => {
    getTasks.mockResolvedValue([
      makeTask(),
      makeTask({ id: "t2", epicId: null, title: "Not in this epic" }),
    ]);
    render(await EpicPage({ params }));
    expect(getTasks).toHaveBeenCalledWith("p1");
    expect(screen.getByText("Build the list screen")).toBeInTheDocument();
    expect(screen.queryByText("Not in this epic")).not.toBeInTheDocument();
  });

  it("reports a task load failure without failing the whole page", async () => {
    getTasks.mockRejectedValue(new Error("boom"));
    render(await EpicPage({ params }));
    expect(screen.getByRole("heading", { name: "Screens" })).toBeInTheDocument();
    expect(screen.getByText(/Failed to load/)).toBeInTheDocument();
  });

  // The epic sets neither, so both fall through to its backlog — and the view
  // has to say which of the two the reader is looking at.
  it("resolves an unset base branch and scope from the backlog", async () => {
    render(await EpicPage({ params }));
    expect(screen.getByText("main")).toBeInTheDocument();
    expect(screen.getByText("apps/**")).toBeInTheDocument();
    expect(screen.getAllByText(/\(from backlog\)/).length).toBeGreaterThan(0);
  });
});
