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
  progress: "not_started",
  defaultLinkedGitlabProjectId: null,
  baseBranch: "",
  allowedScope: "",
  assigneeUserId: null,
  assigneeUsername: "",
  assigneeDisplayName: "",
  forbiddenScope: "",
  taskCount: 0,
  closedTaskCount: 0,
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
  getBacklogs: (id: string, filter?: unknown) => getBacklogs(id, filter),
  getTasks: (id: string, filter?: unknown) => getTasks(id, filter),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/p1/backlogs",
  useSearchParams: () => new URLSearchParams(),
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

  it("renders the backlog collection", async () => {
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByRole("link", { name: /Sprint 1/ })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs/b1",
    );
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

  // issue #155: a getBacklogs failure is caught in-page, the same as
  // getTasks in tasks/page.tsx, rather than bubbling up to error.tsx and
  // taking the rest of the screen (New backlog) down with it.
  it("shows a load error without failing the whole page when backlogs fail to load", async () => {
    getBacklogs.mockRejectedValue(new Error("boom"));
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("Failed to load backlogs. Try refreshing the page.")).toBeInTheDocument();
  });

  // The per-backlog count is aggregated server-side onto the backlog itself
  // (issue #144), so the collection renders it straight from getBacklogs
  // without a separate task fetch.
  it("shows each backlog's completion in the List view mode", async () => {
    getBacklogs.mockResolvedValue([{ ...backlog, taskCount: 5, closedTaskCount: 3 }]);
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    // The per-backlog completion lives in the List view mode; Board is the default.
    fireEvent.click(screen.getByRole("button", { name: "List" }));
    expect(screen.getByText("3/5 closed")).toBeInTheDocument();
  });

  // issue #152: the Unclassified count comes from a separate getTasks call
  // (backlog_id=unassigned is the API's value for "no backlog", distinct
  // from UNCLASSIFIED_BACKLOG which is this app's own route/filter value),
  // and is passed through to the collection as a trailing List-only row.
  it("fetches the unclassified task count and shows it as a trailing List row", async () => {
    getTasks.mockResolvedValue([{ id: "t1" }, { id: "t2" }]);
    render(await BacklogsPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(getTasks).toHaveBeenCalledWith("p1", { backlogId: "unassigned" });

    fireEvent.click(screen.getByRole("button", { name: "List" }));
    const unclassified = screen.getByRole("link", { name: /Unclassified/ });
    expect(unclassified).toHaveTextContent("(2)");
    expect(unclassified).toHaveAttribute("href", "/projects/p1/tasks?backlog=unclassified");
  });

  // ?priority=/?progress=/?sort= (issue #151) go through to the API the same
  // way the Task collection's own filters do.
  it("forwards ?priority=/?progress=/?sort= to getBacklogs", async () => {
    render(
      await BacklogsPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ priority: "urgent", progress: "on_hold", sort: "priority" }),
      }),
    );
    expect(getBacklogs).toHaveBeenCalledWith("p1", {
      priority: "urgent",
      progress: "on_hold",
      sort: "priority",
    });
  });

  // sort=dueOn has no server-side meaning (parseBacklogListFilter only
  // accepts priority/progress) — BacklogListSection sorts it itself, so it
  // must never reach the API as ?sort=.
  it("does not forward sort=dueOn to getBacklogs, since the API doesn't accept it", async () => {
    render(
      await BacklogsPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ sort: "dueOn" }),
      }),
    );
    expect(getBacklogs).toHaveBeenCalledWith("p1", {
      priority: undefined,
      progress: undefined,
      sort: undefined,
    });
  });

  it("falls back to no filter for an unrecognised ?priority=", async () => {
    render(
      await BacklogsPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ priority: "not-a-priority" }),
      }),
    );
    expect(getBacklogs).toHaveBeenCalledWith("p1", {
      priority: undefined,
      progress: undefined,
      sort: undefined,
    });
  });

  describe("View mode in the URL (issue #153)", () => {
    it("opens in the view named by ?view=", async () => {
      render(
        await BacklogsPage({
          params: Promise.resolve({ projectId: "p1" }),
          searchParams: Promise.resolve({ view: "timeline" }),
        }),
      );
      expect(screen.getByRole("button", { name: "Timeline" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    });

    it("falls back to Board for an unrecognised ?view=", async () => {
      render(
        await BacklogsPage({
          params: Promise.resolve({ projectId: "p1" }),
          searchParams: Promise.resolve({ view: "kanban" }),
        }),
      );
      expect(screen.getByRole("button", { name: "Board" })).toHaveAttribute("aria-pressed", "true");
    });
  });
});
