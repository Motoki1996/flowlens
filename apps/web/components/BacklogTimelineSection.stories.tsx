import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, within } from "storybook/test";
import { BacklogTimelineSection } from "./BacklogTimelineSection";
import type { Backlog, Task } from "@/types";

/** Every story pins "today" so the overdue bars and the Today marker land in
 *  the same place on every run — otherwise the chart drifts with the clock and
 *  no visual snapshot of it is stable. */
const NOW = new Date("2026-08-10T00:00:00Z");

function makeBacklog(overrides: Partial<Backlog>): Backlog {
  return {
    id: "b1",
    projectId: "p1",
    name: "Backlog",
    description: "",
    position: 0,
    startDate: null,
    dueOn: null,
    priority: "medium",
    progress: "not_started",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: "b1",
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

/** tasksFor builds `closed` closed tasks and `open` open ones in a backlog,
 *  which is all a bar's fill depends on. */
function tasksFor(backlogId: string, closed: number, open: number): Task[] {
  return [
    ...Array.from({ length: closed }, (_, i) =>
      makeTask({ id: `${backlogId}-c${i}`, backlogId, status: "closed" }),
    ),
    ...Array.from({ length: open }, (_, i) =>
      makeTask({ id: `${backlogId}-o${i}`, backlogId, status: "open" }),
    ),
  ];
}

const quarter: Backlog[] = [
  makeBacklog({ id: "b1", name: "Sprint 1", startDate: "2026-07-27", dueOn: "2026-08-07" }),
  makeBacklog({ id: "b2", name: "Sprint 2", startDate: "2026-08-10", dueOn: "2026-08-21" }),
  makeBacklog({ id: "b3", name: "Hardening", startDate: "2026-08-24", dueOn: "2026-09-04" }),
];

const meta = {
  title: "Components/BacklogTimelineSection",
  component: BacklogTimelineSection,
  args: { projectId: "p1", now: NOW },
} satisfies Meta<typeof BacklogTimelineSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    backlogs: quarter,
    tasks: [...tasksFor("b1", 5, 1), ...tasksFor("b2", 2, 6), ...tasksFor("b3", 0, 4)],
  },
};

/** The empty state of this view mode is "nothing is scheduled" rather than "no
 *  backlogs" — the collection can be full and the chart still have nothing to
 *  plot. */
export const Empty: Story = {
  args: { backlogs: [makeBacklog({ id: "b1", name: "Someday" })], tasks: [] },
};

/** A fully closed backlog recedes to muted; one still carrying open work past
 *  its due date turns destructive-red. Colour never carries this alone — the
 *  legend names it and the tooltip repeats it. */
export const CompleteAndOverdue: Story = {
  args: {
    backlogs: [
      makeBacklog({ id: "b1", name: "Shipped", startDate: "2026-07-27", dueOn: "2026-08-05" }),
      makeBacklog({ id: "b2", name: "Slipping", startDate: "2026-07-30", dueOn: "2026-08-07" }),
      makeBacklog({ id: "b3", name: "On track", startDate: "2026-08-10", dueOn: "2026-08-21" }),
    ],
    tasks: [...tasksFor("b1", 4, 0), ...tasksFor("b2", 1, 3), ...tasksFor("b3", 0, 5)],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByText("Overdue")).toBeInTheDocument();
  },
};

/** A backlog with no tasks yet is not "complete" — its bar stays unfilled and
 *  the row says so in words. */
export const WithoutTasks: Story = {
  args: { backlogs: quarter, tasks: [] },
};

/** Backlogs with neither date are named below the chart instead of being
 *  dropped silently, so the collection count still reconciles. */
export const WithUnscheduledBacklogs: Story = {
  args: {
    backlogs: [...quarter, makeBacklog({ id: "b9", name: "Someday / maybe" })],
    tasks: tasksFor("b1", 3, 3),
  },
};

/** Progress is unknowable when the task fetch fails, so the chart says that
 *  rather than drawing every backlog as 0% done. */
export const TasksFailedToLoad: Story = {
  args: { backlogs: quarter, tasks: [], tasksError: true },
};
