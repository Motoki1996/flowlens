import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { TaskDetail } from "./TaskDetail";
import { API_PUBLIC_URL } from "@/lib/config";
import type { Backlog, Task } from "@/types";

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "",
  position: 0,
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

/** 未連携: the task's project has never had a linked GitLab project. */
export const Unlinked: Story = {
  args: { task: makeTask({ gitlab: null }), backlogs: [backlog] },
};

/** 同期済み: the task pushed cleanly and links to its GitLab issue. */
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
  },
};

/** 同期中: a push is enqueued but hasn't been picked up by the worker yet. */
export const Pending: Story = {
  args: {
    task: makeTask({
      gitlab: { syncStatus: "pending", lastError: "", lastSyncedAt: null, issueIid: null, webUrl: "" },
    }),
    backlogs: [backlog],
  },
};

/** 失敗（エラー文言あり）: the last push failed; the error and a retry action are shown. */
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
