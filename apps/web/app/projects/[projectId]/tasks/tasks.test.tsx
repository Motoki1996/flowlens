import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Project, User } from "@/types";

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

const getCurrentUser = vi.fn();
const getProject = vi.fn();
const getTasks = vi.fn();
const getBacklogs = vi.fn();
const getEpics = vi.fn();
const getTaskDependencies = vi.fn();
const getGitlabConnection = vi.fn();
const getMyGitlabIdentities = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getProject: (id: string) => getProject(id),
  getTasks: (id: string, filter?: unknown) => getTasks(id, filter),
  getBacklogs: (id: string) => getBacklogs(id),
  getEpics: (id: string) => getEpics(id),
  getTaskDependencies: (id: string) => getTaskDependencies(id),
  getGitlabConnection: (id: string) => getGitlabConnection(id),
  getMyGitlabIdentities: () => getMyGitlabIdentities(),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/projects/p1/tasks",
  useSearchParams: () => new URLSearchParams(),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import TasksPage from "./page";
import { taskPage } from "@/lib/test-pages";

describe("TasksPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue(user);
    getProject.mockResolvedValue(project);
    getTasks.mockResolvedValue(taskPage([]));
    getBacklogs.mockResolvedValue([]);
    getEpics.mockResolvedValue([]);
    getTaskDependencies.mockResolvedValue([]);
    getGitlabConnection.mockResolvedValue(null);
    getMyGitlabIdentities.mockResolvedValue([]);
  });

  it("renders the task collection, asking the API for open tasks by default", async () => {
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("Tasks")).toBeInTheDocument();
    // Only status is sent: every other filter is off, and the absent sort is
    // the API's own manual/position order.
    expect(getTasks).toHaveBeenCalledWith("p1", {
      backlogId: undefined,
      epicId: undefined,
      status: "open",
      assignee: undefined,
      sort: undefined,
      q: undefined,
      page: 1,
      perPage: 100,
    });
    expect(getBacklogs).toHaveBeenCalledWith("p1");
  });

  it("passes the URL query to the API as the request's filter (issue #143)", async () => {
    getBacklogs.mockResolvedValue([
      { id: "b1", projectId: "p1", name: "Sprint 1", description: "" },
    ]);
    getTasks.mockResolvedValue(taskPage([
      {
        id: "t1",
        projectId: "p1",
        backlogId: "b1",
        title: "In sprint 1",
        status: "open",
        labels: [],
        priority: "medium",
        progress: "in_progress",
        size: "m",
      },
    ]));
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({
          backlog: "8f0f2c1e-2c9c-4b1f-9f4a-0d5b1f6c7a21",
          status: "all",
          assignee: "me",
          sort: "priority",
          q: "sprint",
        }),
      }),
    );

    expect(getTasks).toHaveBeenCalledWith("p1", {
      backlogId: "8f0f2c1e-2c9c-4b1f-9f4a-0d5b1f6c7a21",
      epicId: undefined,
      status: undefined,
      assignee: "me",
      sort: "priority",
      q: "sprint",
      page: 1,
      perPage: 100,
    });
    expect(screen.getByText("In sprint 1")).toBeInTheDocument();
  });

  it("passes the project's unfiltered task count through as the result total (issue #150)", async () => {
    // getTasks is called twice: once with the applied filter (always at
    // least `status: "open"` by default) and once with `{}` for the
    // unfiltered total — distinguished here by whether any filter key is
    // actually set.
    // perPage: 1 is the screen's 母数 fetch — the project's count with no
    // filter applied; every other call is the filtered page it renders.
    getTasks.mockImplementation((_id: string, filter: Record<string, unknown> = {}) =>
      Promise.resolve(filter.perPage === 1 ? taskPage([{ id: "t1" }, { id: "t2" }]) : taskPage([])),
    );
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("0 / 2 tasks")).toBeInTheDocument();
  });

  it("sends the Unclassified group as the API's backlog_id=unassigned", async () => {
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ backlog: "unclassified" }),
      }),
    );
    expect(getTasks).toHaveBeenCalledWith("p1", expect.objectContaining({ backlogId: "unassigned" }));
  });

  it("falls back to the defaults for query values the API would reject", async () => {
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({
          backlog: "not-a-uuid",
          status: "banana",
          sort: "sideways",
        }),
      }),
    );
    expect(getTasks).toHaveBeenCalledWith("p1", {
      backlogId: undefined,
      epicId: undefined,
      status: "open",
      assignee: undefined,
      sort: undefined,
      q: undefined,
      page: 1,
      perPage: 100,
    });
  });

  // The page number rides in the URL like every other filter, so a paged
  // result stays shareable and the back button walks it.
  it("forwards ?page= to the API and hands the API's paging back to the list", async () => {
    getTasks.mockResolvedValue(taskPage([{ id: "t1", title: "On page two" }], { nextPage: 3, totalCount: 250 }));
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ page: "2" }),
      }),
    );
    expect(getTasks).toHaveBeenCalledWith("p1", expect.objectContaining({ page: 2, perPage: 100 }));
    expect(screen.getByRole("navigation", { name: "Pagination" })).toBeInTheDocument();
  });

  it("pre-selects the backlog filter from ?backlog=", async () => {
    const backlogId = "8f0f2c1e-2c9c-4b1f-9f4a-0d5b1f6c7a21";
    getBacklogs.mockResolvedValue([
      { id: backlogId, projectId: "p1", name: "Sprint 1", description: "" },
    ]);
    render(
      await TasksPage({
        params: Promise.resolve({ projectId: "p1" }),
        searchParams: Promise.resolve({ backlog: backlogId }),
      }),
    );
    expect(screen.getByRole("combobox", { name: "Backlog" })).toHaveTextContent("Sprint 1");
  });

  describe("View mode in the URL (issue #153)", () => {
    it("opens in the view named by ?view=", async () => {
      render(
        await TasksPage({
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
        await TasksPage({
          params: Promise.resolve({ projectId: "p1" }),
          searchParams: Promise.resolve({ view: "kanban" }),
        }),
      );
      expect(screen.getByRole("button", { name: "Board" })).toHaveAttribute("aria-pressed", "true");
    });
  });

  it("renders not-found when the project doesn't exist", async () => {
    getProject.mockResolvedValue(null);
    await expect(TasksPage({ params: Promise.resolve({ projectId: "unknown" }) })).rejects.toThrow(
      "NOT_FOUND",
    );
  });

  it("shows a load error without failing the whole page when tasks fail to load", async () => {
    getTasks.mockRejectedValue(new Error("boom"));
    render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
  });

  describe("Due date state (issue #148)", () => {
    // The page computes "today" itself (rather than the client component
    // doing it with `new Date()`) so a task's Overdue state can't disagree
    // between the server render and the browser hydrating it — see
    // TaskListSection's `today` prop doc comment.
    afterEach(() => {
      vi.useRealTimers();
    });

    it("marks a task due before the page's own current date as Overdue", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date(2026, 7, 5, 12, 0, 0));
      getTasks.mockResolvedValue(taskPage([
        {
          id: "t1",
          projectId: "p1",
          backlogId: null,
          title: "Late task",
          status: "open",
          labels: [],
          priority: "medium",
          progress: "not_started",
          size: "m",
          dueOn: "2026-08-04",
        },
      ]));
      render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
      expect(screen.getByText("Overdue Aug 4, 2026")).toBeInTheDocument();
    });
  });

  describe("My tasks availability (issue #146)", () => {
    it("disables the checkbox and points at the GitLab connection when the project has none", async () => {
      getGitlabConnection.mockResolvedValue(null);
      render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
      expect(screen.getByRole("checkbox", { name: "My tasks" })).toBeDisabled();
      expect(screen.getByRole("link", { name: "Connect GitLab" })).toHaveAttribute(
        "href",
        "/projects/p1/gitlab-connection",
      );
      // A connectionless project has no identity to match either, so the
      // caller's own identities aren't even worth fetching.
      expect(getMyGitlabIdentities).not.toHaveBeenCalled();
    });

    it("disables the checkbox and points at Settings when the caller has no matching identity", async () => {
      getGitlabConnection.mockResolvedValue({
        projectId: "p1",
        baseUrl: "https://gitlab.example.com",
        tokenLastFour: "abcd",
        tokenGitlabUserId: null,
        tokenGitlabUsername: "",
        verified: true,
        lastVerifiedAt: null,
        lastVerifyError: "",
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      });
      getMyGitlabIdentities.mockResolvedValue([
        { id: "i1", gitlabBaseUrl: "https://other.example.com", gitlabUserId: 1, gitlabUsername: "me" },
      ]);
      render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
      expect(screen.getByRole("checkbox", { name: "My tasks" })).toBeDisabled();
      expect(screen.getByRole("link", { name: "Register GitLab identity" })).toHaveAttribute(
        "href",
        "/settings",
      );
    });

    it("enables the checkbox once the caller has an identity matching the project's GitLab connection", async () => {
      getGitlabConnection.mockResolvedValue({
        projectId: "p1",
        baseUrl: "https://gitlab.example.com",
        tokenLastFour: "abcd",
        tokenGitlabUserId: null,
        tokenGitlabUsername: "",
        verified: true,
        lastVerifiedAt: null,
        lastVerifyError: "",
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      });
      getMyGitlabIdentities.mockResolvedValue([
        {
          id: "i1",
          gitlabBaseUrl: "https://gitlab.example.com",
          gitlabUserId: 1,
          gitlabUsername: "me",
        },
      ]);
      render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
      expect(screen.getByRole("checkbox", { name: "My tasks" })).not.toBeDisabled();
    });

    it("leaves the checkbox disabled rather than failing the page when the lookup errors", async () => {
      getGitlabConnection.mockRejectedValue(new Error("boom"));
      render(await TasksPage({ params: Promise.resolve({ projectId: "p1" }) }));
      expect(screen.getByText("Tasks")).toBeInTheDocument();
      expect(screen.getByRole("checkbox", { name: "My tasks" })).toBeDisabled();
    });
  });
});
