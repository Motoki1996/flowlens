import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, within } from "storybook/test";
import { BacklogTimelineSection } from "./BacklogTimelineSection";
import type { Backlog } from "@/types";

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
    defaultLinkedGitlabProjectId: null,
    baseBranch: "",
    allowedScope: "",
    forbiddenScope: "",
    taskCount: 0,
    closedTaskCount: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const quarter: Backlog[] = [
  makeBacklog({
    id: "b1",
    name: "Sprint 1",
    startDate: "2026-07-27",
    dueOn: "2026-08-07",
    taskCount: 6,
    closedTaskCount: 5,
  }),
  makeBacklog({
    id: "b2",
    name: "Sprint 2",
    startDate: "2026-08-10",
    dueOn: "2026-08-21",
    taskCount: 8,
    closedTaskCount: 2,
  }),
  makeBacklog({
    id: "b3",
    name: "Hardening",
    startDate: "2026-08-24",
    dueOn: "2026-09-04",
    taskCount: 4,
    closedTaskCount: 0,
  }),
];

const meta = {
  title: "Components/BacklogTimelineSection",
  component: BacklogTimelineSection,
  args: { projectId: "p1", now: NOW },
} satisfies Meta<typeof BacklogTimelineSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { backlogs: quarter },
};

/** The empty state of this view mode is "nothing is scheduled" rather than "no
 *  backlogs" — the collection can be full and the chart still have nothing to
 *  plot. */
export const Empty: Story = {
  args: { backlogs: [makeBacklog({ id: "b1", name: "Someday" })] },
};

/** A fully closed backlog recedes to muted; one still carrying open work past
 *  its due date turns destructive-red. Colour never carries this alone — the
 *  legend names it and the tooltip repeats it. */
export const CompleteAndOverdue: Story = {
  args: {
    backlogs: [
      makeBacklog({
        id: "b1",
        name: "Shipped",
        startDate: "2026-07-27",
        dueOn: "2026-08-05",
        taskCount: 4,
        closedTaskCount: 4,
      }),
      makeBacklog({
        id: "b2",
        name: "Slipping",
        startDate: "2026-07-30",
        dueOn: "2026-08-07",
        taskCount: 4,
        closedTaskCount: 1,
      }),
      makeBacklog({
        id: "b3",
        name: "On track",
        startDate: "2026-08-10",
        dueOn: "2026-08-21",
        taskCount: 5,
        closedTaskCount: 0,
      }),
    ],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByText("Overdue")).toBeInTheDocument();
  },
};

/** A backlog with no tasks yet is not "complete" — its bar stays unfilled and
 *  the row says so in words. */
export const WithoutTasks: Story = {
  args: { backlogs: quarter.map((b) => ({ ...b, taskCount: 0, closedTaskCount: 0 })) },
};

/** Backlogs with neither date are named below the chart instead of being
 *  dropped silently, so the collection count still reconciles. */
export const WithUnscheduledBacklogs: Story = {
  args: {
    backlogs: [...quarter, makeBacklog({ id: "b9", name: "Someday / maybe" })],
  },
};
