import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { BacklogDetail } from "./BacklogDetail";
import type { Backlog, LinkedGitlabProject, Task } from "@/types";

const project = { id: "p1", name: "Alpha" };

const links: LinkedGitlabProject[] = [
  {
    id: "l1",
    gitlabConnectionId: "c1",
    gitlabProjectId: 100,
    pathWithNamespace: "group/demo",
    name: "demo",
    webUrl: "https://gitlab.example.com/group/demo",
    syncScope: "all",
    syncLabels: [],
    isDefault: true,
    initialImportStatus: "completed",
    lastSyncedAt: null,
    webhookStatus: "registered",
    webhookRegisteredAt: null,
    webhookError: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  },
  {
    id: "l2",
    gitlabConnectionId: "c1",
    gitlabProjectId: 200,
    pathWithNamespace: "group/other",
    name: "other",
    webUrl: "https://gitlab.example.com/group/other",
    syncScope: "all",
    syncLabels: [],
    isDefault: false,
    initialImportStatus: "completed",
    lastSyncedAt: null,
    webhookStatus: "registered",
    webhookRegisteredAt: null,
    webhookError: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  },
];

const backlog: Backlog = {
  id: "b1",
  projectId: "p1",
  name: "Sprint 1",
  description: "Two-week sprint ending Friday.",
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
  status: "open",
  closedAt: null,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-02T00:00:00Z",
};

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
    epicId: null,
    title: "Fix the bug",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "octocat",
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

/** A backlog that uses the epic rung: the card opens on Epics rather than
 *  Tasks, and "New epic" files a new one in this backlog without leaving the
 *  screen. Both lists are the same card, one tab apart. */
export const WithEpics: Story = {
  args: {
    backlog,
    project,
    epics: [
      {
        id: "e1",
        projectId: "p1",
        backlogId: "b1",
        name: "Screens",
        description: "",
        startDate: null,
        dueOn: null,
        priority: "medium",
        progress: "in_progress",
        defaultLinkedGitlabProjectId: null,
        baseBranch: "release/2.4",
        allowedScope: "",
        forbiddenScope: "",
        estimatedPoints: null,
        assigneeUserId: null,
        assigneeUsername: "",
        assigneeDisplayName: "",
        taskCount: 6,
        closedTaskCount: 4,
        status: "open",
        closedAt: null,
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      },
      {
        id: "e2",
        projectId: "p1",
        backlogId: "b1",
        name: "API endpoints",
        description: "",
        startDate: null,
        dueOn: null,
        priority: "high",
        progress: "not_started",
        defaultLinkedGitlabProjectId: null,
        baseBranch: "",
        allowedScope: "",
        forbiddenScope: "",
        estimatedPoints: null,
        assigneeUserId: null,
        assigneeUsername: "",
        assigneeDisplayName: "",
        taskCount: 3,
        closedTaskCount: 0,
        status: "open",
        closedAt: null,
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      },
    ],
    tasks: [
      makeTask({ id: "t1", title: "Fix the bug" }),
      makeTask({ id: "t2", title: "Write docs", status: "closed", assigneeGitlabUsername: "" }),
    ],
  },
};

/** A backlog that files its tasks' issues in a GitLab project of its own,
 *  rather than following the project's default link (issue #180). */
export const WithOwnGitlabProject: Story = {
  args: {
    backlog: { ...backlog, defaultLinkedGitlabProjectId: "l2" },
    project,
    tasks: [],
    links,
  },
};

/** A Markdown description, rendered the same way a task's is. */
export const MarkdownDescription: Story = {
  args: {
    backlog: {
      ...backlog,
      description:
        "Ends Friday. Scope is in the [plan](https://example.com/plan).\n\n- [x] Kickoff\n- [ ] Review\n\nQuestions to https://gitlab.example.com/group/demo",
    },
    project,
    tasks: [],
  },
};

export const Error: Story = {
  args: { backlog, project, tasks: [], tasksError: true },
};
