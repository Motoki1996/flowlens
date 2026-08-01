import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { BacklogDetail } from "./BacklogDetail";
import type { Backlog, Task } from "@/types";

const project = { id: "p1", name: "Alpha" };

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "Two-week sprint ending Friday.",
  position: 0,
  startDate: null,
  dueOn: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-02T00:00:00Z",
};

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    title: "Fix the bug",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "octocat",
    labels: [],
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
  title: "Screens/Backlog",
  component: BacklogDetail,
} satisfies Meta<typeof BacklogDetail>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  args: { backlog, project, tasks: [] },
};

export const WithTasks: Story = {
  args: {
    backlog,
    project,
    tasks: [
      makeTask({ id: "t1", title: "Fix the bug" }),
      makeTask({ id: "t2", title: "Write docs", status: "closed", assigneeGitlabUsername: "" }),
    ],
  },
};

export const Error: Story = {
  args: { backlog, project, tasks: [], tasksError: true },
};
