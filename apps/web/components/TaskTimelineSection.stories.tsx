import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, within } from "storybook/test";
import { TaskTimelineSection } from "./TaskTimelineSection";
import type { Task, TaskDependency } from "@/types";

/** Every story pins "today" so the overdue bars and the Today marker land in
 *  the same place on every run — otherwise the chart drifts with the clock and
 *  no visual snapshot of it is stable. */
const NOW = new Date("2026-08-10T00:00:00Z");

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: null,
    epicId: null,
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

const sprint: Task[] = [
  makeTask({
    id: "t1",
    title: "Design the sync worker",
    startDate: "2026-08-03",
    dueOn: "2026-08-06",
    status: "closed",
  }),
  makeTask({ id: "t2", title: "Outbox schema migration", startDate: "2026-08-05", dueOn: "2026-08-08" }),
  makeTask({ id: "t3", title: "Webhook receiver", startDate: "2026-08-07", dueOn: "2026-08-14" }),
  makeTask({ id: "t4", title: "Task context API", startDate: "2026-08-12", dueOn: "2026-08-19" }),
];

const meta = {
  title: "Components/TaskTimelineSection",
  component: TaskTimelineSection,
  args: { projectId: "p1", now: NOW },
} satisfies Meta<typeof TaskTimelineSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { tasks: sprint, dependencies: [] },
};

/** The empty state of this view mode is "nothing is scheduled" rather than
 *  "no tasks" — the collection can be full and the chart still have nothing to
 *  plot. */
export const Empty: Story = {
  args: {
    tasks: [makeTask({ id: "t1", title: "Not scheduled yet" })],
    dependencies: [],
  },
};

export const WithDependencies: Story = {
  args: {
    tasks: sprint,
    dependencies: [
      { id: "d1", predecessorTaskId: "t1", successorTaskId: "t2", createdAt: "2026-01-01T00:00:00Z" },
      { id: "d2", predecessorTaskId: "t2", successorTaskId: "t3", createdAt: "2026-01-01T00:00:00Z" },
      { id: "d3", predecessorTaskId: "t3", successorTaskId: "t4", createdAt: "2026-01-01T00:00:00Z" },
    ] satisfies TaskDependency[],
  },
};

/** An open task whose due date has passed turns destructive-red. Colour never
 *  carries this alone — the legend names it and the tooltip repeats it. */
export const Overdue: Story = {
  args: {
    tasks: [
      makeTask({ id: "t1", title: "Should have shipped", startDate: "2026-08-01", dueOn: "2026-08-04" }),
      makeTask({ id: "t2", title: "On track", startDate: "2026-08-08", dueOn: "2026-08-14" }),
      makeTask({ id: "t3", title: "Done", startDate: "2026-08-02", dueOn: "2026-08-05", status: "closed" }),
    ],
    dependencies: [],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByText("Overdue")).toBeInTheDocument();
  },
};

/** Past three weeks the timeline opens at weekly ticks, and the plot scrolls
 *  horizontally rather than compressing the bars. The Zoom control overrides
 *  that default in either direction; Today scrolls the plot back to the marker. */
export const LongRange: Story = {
  args: {
    tasks: [
      makeTask({ id: "t1", title: "Discovery", startDate: "2026-07-01", dueOn: "2026-07-24" }),
      makeTask({ id: "t2", title: "Build", startDate: "2026-07-20", dueOn: "2026-09-04" }),
      makeTask({ id: "t3", title: "Rollout", startDate: "2026-09-01", dueOn: "2026-09-30" }),
    ],
    dependencies: [],
  },
};

/** A year-long plan opens at monthly ticks — the whole range is legible at a
 *  glance, and zooming to Day expands it into a scrollable day-by-day view
 *  without ever hiding a task. */
export const YearLongPlan: Story = {
  args: {
    tasks: [
      makeTask({ id: "t1", title: "Discovery", startDate: "2026-01-05", dueOn: "2026-03-31", status: "closed" }),
      makeTask({ id: "t2", title: "Issue sync MVP", startDate: "2026-03-01", dueOn: "2026-07-15" }),
      makeTask({ id: "t3", title: "Delivery-flow visualisation", startDate: "2026-07-01", dueOn: "2026-11-30" }),
      makeTask({ id: "t4", title: "GA", startDate: "2026-11-15", dueOn: "2026-12-20" }),
    ],
    dependencies: [],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("button", { name: "Month" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  },
};

/** Tasks with neither date are named below the chart instead of being dropped
 *  silently, so the collection count still reconciles. */
export const WithUnscheduledTasks: Story = {
  args: {
    tasks: [...sprint, makeTask({ id: "t9", title: "Backlog idea" })],
    dependencies: [],
  },
};
