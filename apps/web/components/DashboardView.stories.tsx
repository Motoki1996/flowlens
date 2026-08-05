import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { DashboardView } from "./DashboardView";
import type { Project, TaskWithProject } from "@/types";

function makeTask(overrides: Partial<TaskWithProject>): TaskWithProject {
  return {
    id: "t1",
    projectId: "p1",
    projectName: "Payments",
    backlogId: null,
    title: "Task",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
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

const project: Project = {
  id: "p1",
  name: "Payments",
  description: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-08-01T00:00:00Z",
  failedSyncTaskCount: 0,
};

const meta = {
  title: "Screens/Dashboard",
  component: DashboardView,
} satisfies Meta<typeof DashboardView>;

export default meta;
type Story = StoryObj<typeof meta>;

const baseArgs = {
  hasProjects: true,
  showDueDateHint: false,
  overdueHref: "/tasks?status=open&sort=dueOn&dueBefore=2026-08-02",
  dueSoonHref: "/tasks?status=open&sort=dueOn&dueAfter=2026-08-03&dueBefore=2026-08-09",
  waitingHref: "/tasks?status=open&sort=dueOn&startedBefore=2026-08-03",
  priorityHref: "/tasks?status=open&sort=priority",
};

export const Default: Story = {
  args: {
    ...baseArgs,
    overdueTasks: [makeTask({ id: "t1", title: "Renew GitLab token", dueOn: "2026-07-28" })],
    dueSoonTasks: [
      makeTask({ id: "t2", title: "Ship pricing page", dueOn: "2026-08-05", priority: "high" }),
    ],
    waitingTasks: [
      makeTask({ id: "t3", title: "Kick off Q3 migration", startDate: "2026-08-01" }),
    ],
    priorityTasks: [makeTask({ id: "t4", title: "Fix checkout crash", priority: "urgent" })],
    failedSyncProjects: [{ ...project, failedSyncTaskCount: 3 }],
    recentProjects: [project, { ...project, id: "p2", name: "Onboarding" }],
  },
};

export const Empty: Story = {
  args: {
    ...baseArgs,
    overdueTasks: [],
    dueSoonTasks: [],
    waitingTasks: [],
    priorityTasks: [],
    failedSyncProjects: [],
    recentProjects: [project],
  },
};

/** Tasks exist, but none has a due date yet — the empty state explains what
 *  setting one would surface instead of implying nothing is overdue. */
export const NoDueDates: Story = {
  args: {
    ...baseArgs,
    showDueDateHint: true,
    overdueTasks: [],
    dueSoonTasks: [],
    waitingTasks: [],
    priorityTasks: [],
    failedSyncProjects: [],
    recentProjects: [project],
  },
};

export const NoProjects: Story = {
  args: {
    ...baseArgs,
    hasProjects: false,
    overdueTasks: [],
    dueSoonTasks: [],
    waitingTasks: [],
    priorityTasks: [],
    failedSyncProjects: [],
    recentProjects: [],
  },
};

export const Error: Story = {
  args: {
    ...baseArgs,
    error: true,
    overdueTasks: [],
    dueSoonTasks: [],
    waitingTasks: [],
    priorityTasks: [],
    failedSyncProjects: [],
    recentProjects: [],
  },
};
