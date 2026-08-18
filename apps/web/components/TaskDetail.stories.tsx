import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { TaskDetail } from "./TaskDetail";
import { API_PUBLIC_URL } from "@/lib/config";
import type { Backlog, Task, TaskDependency } from "@/types";

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
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    title: "Fix the bug",
    description: "Details about the bug.",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "octocat",
    labels: ["bug"],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
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
      allowedScope: "",
      forbiddenScope: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

const meta = {
  title: "Screens/Task",
  component: TaskDetail,
} satisfies Meta<typeof TaskDetail>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Not linked: the task's project has never had a linked GitLab project. */
export const Unlinked: Story = {
  args: { task: makeTask({ gitlab: null }), backlogs: [backlog], tasks: [], dependencies: [] },
};

/** Synced: the task pushed cleanly and links to its GitLab issue. */
export const Synced: Story = {
  args: {
    task: makeTask({
      gitlab: {
        syncStatus: "synced",
        lastError: "",
        lastSyncedAt: "2026-01-05T09:00:00Z",
        issueIid: 42,
        webUrl: "https://gitlab.example.com/group/demo/-/issues/42",
      },
    }),
    backlogs: [backlog],
    tasks: [],
    dependencies: [],
  },
};

/** Syncing: a push is enqueued but hasn't been picked up by the worker yet. */
export const Pending: Story = {
  args: {
    task: makeTask({
      gitlab: { syncStatus: "pending", lastError: "", lastSyncedAt: null, issueIid: null, webUrl: "" },
    }),
    backlogs: [backlog],
    tasks: [],
    dependencies: [],
  },
};

/** Failed (with an error message): the last push failed; the error and a retry action are shown. */
export const Failed: Story = {
  args: {
    task: makeTask({
      gitlab: {
        syncStatus: "failed",
        lastError: "gitlab rejected the update: the personal access token was revoked",
        lastSyncedAt: "2026-01-04T09:00:00Z",
        issueIid: 42,
        webUrl: "https://gitlab.example.com/group/demo/-/issues/42",
      },
    }),
    backlogs: [backlog],
    tasks: [],
    dependencies: [],
  },
};

const otherTasks: Task[] = [
  makeTask({ id: "t2", title: "Design the fix" }),
  makeTask({ id: "t3", title: "Ship the fix" }),
];

const dependencies: TaskDependency[] = [
  { id: "d1", predecessorTaskId: "t2", successorTaskId: "t1", createdAt: "2026-01-01T00:00:00Z" },
  { id: "d2", predecessorTaskId: "t1", successorTaskId: "t3", createdAt: "2026-01-01T00:00:00Z" },
];

/** With dependencies: the task sits between a predecessor and a successor. */
export const WithDependencies: Story = {
  args: {
    task: makeTask({}),
    backlogs: [backlog],
    tasks: [makeTask({}), ...otherTasks],
    dependencies,
  },
};

/** Editing: the attribute block swapped for the inline edit form. */
export const Editing: Story = {
  args: {
    task: makeTask({ dueOn: "2026-02-01T00:00:00Z" }),
    backlogs: [backlog],
    tasks: [],
    dependencies: [],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Edit task" }));
    await expect(await canvas.findByRole("form", { name: "Edit task" })).toBeInTheDocument();
  },
};

/** Edit save failure: the API rejects the edit and the form stays open with the error. */
export const EditFails: Story = {
  args: { task: makeTask({}), backlogs: [backlog], tasks: [], dependencies: [] },
  parameters: {
    msw: {
      handlers: [
        http.patch(`${API_PUBLIC_URL}/api/v1/tasks/:taskId`, () =>
          HttpResponse.json(
            { error: { code: "invalid_title", message: "title must be 1-255 characters" } },
            { status: 400 },
          ),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Edit task" }));
    await userEvent.click(canvas.getByRole("button", { name: "Save" }));
    await expect(await canvas.findByText("title must be 1-255 characters")).toBeInTheDocument();
  },
};

/** Clicking retry on a failed task calls sync-retry and reflects the pending result. */
export const RetrySucceeds: Story = {
  args: {
    task: makeTask({
      gitlab: {
        syncStatus: "failed",
        lastError: "gitlab rejected the update",
        lastSyncedAt: null,
        issueIid: null,
        webUrl: "",
      },
    }),
    backlogs: [backlog],
    tasks: [],
    dependencies: [],
  },
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/api/v1/tasks/:taskId/sync-retry`, () =>
          HttpResponse.json(
            makeTask({
              gitlab: { syncStatus: "pending", lastError: "", lastSyncedAt: null, issueIid: null, webUrl: "" },
            }),
          ),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Retry" }));
    await expect(await canvas.findByText("Syncing…")).toBeInTheDocument();
  },
};
