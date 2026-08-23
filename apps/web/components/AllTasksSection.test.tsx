import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import type { Project, TaskWithProject } from "@/types";
import { AllTasksSection } from "./AllTasksSection";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
  usePathname: () => "/tasks",
  useSearchParams: () => new URLSearchParams(),
}));

function makeTask(overrides: Partial<TaskWithProject>): TaskWithProject {
  return {
    id: "t1",
    projectId: "p1",
    projectName: "Alpha",
    backlogId: null,
    title: "Task",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
    size: "m",
    designStartedAt: null,
    implementationStartedAt: null,
    position: 0,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

const projects: Project[] = [
  { id: "p1", name: "Alpha", description: "", createdAt: "", updatedAt: "", failedSyncTaskCount: 0 },
  { id: "p2", name: "Beta", description: "", createdAt: "", updatedAt: "", failedSyncTaskCount: 0 },
];

describe("AllTasksSection", () => {
  beforeEach(() => {
    push.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows an empty state with zero tasks", () => {
    render(
      <AllTasksSection tasks={[]} projects={projects} status="open" sort="dueOn" />,
    );
    expect(screen.getByText("No tasks match the current filters.")).toBeInTheDocument();
  });

  it("shows a search-specific empty state when a query has no matches", () => {
    render(
      <AllTasksSection
        tasks={[]}
        projects={projects}
        status="open"
        sort="dueOn"
        search="nonexistent"
      />,
    );
    expect(screen.getByText('No tasks match "nonexistent".')).toBeInTheDocument();
  });

  it("pushes ?q= after the search text settles, debounced", () => {
    vi.useFakeTimers();
    const tasks = [makeTask({ dueOn: "2026-02-01T00:00:00Z" })];
    render(<AllTasksSection tasks={tasks} projects={projects} status="open" sort="dueOn" />);

    fireEvent.change(screen.getByRole("textbox", { name: "Search tasks" }), {
      target: { value: "login" },
    });
    expect(push).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(300);
    });

    expect(push).toHaveBeenCalledWith("/tasks?q=login");
  });

  it("shows the error state instead of the task list", () => {
    const tasks = [makeTask({ dueOn: "2026-02-01T00:00:00Z" })];
    render(
      <AllTasksSection tasks={tasks} projects={projects} status="open" sort="dueOn" error />,
    );
    expect(screen.getByText("Failed to load tasks. Try refreshing the page.")).toBeInTheDocument();
    expect(screen.queryByText("Task")).not.toBeInTheDocument();
  });

  it("shows each task's project name alongside its title", () => {
    const tasks = [
      makeTask({ id: "t1", title: "In alpha", projectId: "p1", projectName: "Alpha", dueOn: "2026-02-01T00:00:00Z" }),
      makeTask({ id: "t2", title: "In beta", projectId: "p2", projectName: "Beta", dueOn: "2026-02-02T00:00:00Z" }),
    ];
    render(<AllTasksSection tasks={tasks} projects={projects} status="open" sort="dueOn" />);
    expect(screen.getByText("In alpha")).toBeInTheDocument();
    expect(screen.getByText("In beta")).toBeInTheDocument();
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
  });

  it("hides tasks without a due date by default, and shows them once unchecked", () => {
    const tasks = [
      makeTask({ id: "t1", title: "Dated", dueOn: "2026-02-01T00:00:00Z" }),
      makeTask({ id: "t2", title: "Undated", dueOn: null }),
    ];
    render(<AllTasksSection tasks={tasks} projects={projects} status="open" sort="dueOn" />);

    expect(screen.getByText("Dated")).toBeInTheDocument();
    expect(screen.queryByText("Undated")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: /only with a due date/i }));
    expect(screen.getByText("Undated")).toBeInTheDocument();
  });

  it("pushes a new query string when the project filter changes", async () => {
    const tasks = [makeTask({ dueOn: "2026-02-01T00:00:00Z" })];
    render(<AllTasksSection tasks={tasks} projects={projects} status="open" sort="dueOn" />);

    fireEvent.click(screen.getByRole("combobox", { name: "Project" }));
    fireEvent.click(await screen.findByRole("option", { name: "Beta" }));

    expect(push).toHaveBeenCalledWith("/tasks?projectId=p2");
  });
});
