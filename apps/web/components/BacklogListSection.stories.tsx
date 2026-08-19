import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { BacklogListSection } from "./BacklogListSection";
import type { Backlog } from "@/types";

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
  baseBranch: "",
  taskCount: 0,
  closedTaskCount: 0,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const meta = {
  title: "Components/BacklogListSection",
  component: BacklogListSection,
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: "/projects/p1/backlogs" } },
  },
} satisfies Meta<typeof BacklogListSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  args: { projectId: "p1", backlogs: [] },
};

/** The default view mode: one column per priority, cards stacked inside it. */
export const Default: Story = {
  args: {
    projectId: "p1",
    backlogs: [
      backlog,
      { ...backlog, id: "b2", name: "Hotfixes", priority: "urgent", position: 1 },
      { ...backlog, id: "b3", name: "Icebox", priority: "low", position: 2 },
    ],
  },
};

/** The List view mode, where a backlog is created, edited, deleted and
 *  manually reordered. */
export const List: Story = {
  args: { projectId: "p1", backlogs: [backlog] },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "List" }));
    await expect(canvas.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  },
};

/** Each row's own closed/total completion (issue #152) — the same
 *  `backlogTaskCompletion` reading of taskCount/closedTaskCount the Board
 *  mode's cards already use — plus a trailing "Unclassified" row for tasks
 *  with no backlog, linking to the Task collection's Unclassified group. */
export const ListWithCompletionAndUnclassified: Story = {
  args: {
    projectId: "p1",
    backlogs: [
      { ...backlog, taskCount: 5, closedTaskCount: 2 },
      { ...backlog, id: "b2", name: "Icebox", priority: "low", position: 1 },
    ],
    unclassifiedCount: 3,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "List" }));
    await expect(canvas.getByText("2/5 closed")).toBeInTheDocument();
    await expect(canvas.getByText("No tasks")).toBeInTheDocument();
    const unclassified = canvas.getByRole("link", { name: /Unclassified/ });
    await expect(unclassified).toHaveTextContent("(3)");
    await expect(unclassified).toHaveAttribute("href", "/projects/p1/tasks?backlog=unclassified");
  },
};

/** The Timeline view mode of the same collection, reached from the List/
 *  Timeline toggle. The bars are covered in BacklogTimelineSection's own
 *  stories; this one covers the toggle that gets you there. */
export const Timeline: Story = {
  args: {
    projectId: "p1",
    backlogs: [{ ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" }],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Timeline" }));
    await expect(await canvas.findByRole("list", { name: "Bar colours" })).toBeInTheDocument();
  },
};

/** A priority filter applied (issue #151): the List mode's move buttons and
 *  drag handle disappear, since a filtered request can't include the
 *  project's full backlog set that a reorder round trip requires — and
 *  "Clear filters" appears to undo it. */
export const PriorityFiltered: Story = {
  args: {
    projectId: "p1",
    backlogs: [backlog],
    priorityFilter: "medium",
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "List" }));
    await expect(canvas.getByRole("link", { name: /Sprint 1/ })).toBeInTheDocument();
    await expect(canvas.queryByRole("button", { name: "Move Sprint 1 up" })).not.toBeInTheDocument();
    await expect(canvas.getByRole("button", { name: "Clear filters" })).toBeInTheDocument();
  },
};

/** Sorted by due date (issue #151): the one sort value the API doesn't apply
 *  itself, so BacklogListSection orders the two backlogs client-side —
 *  Icebox (no due date) sorts last. */
export const SortedByDueDate: Story = {
  args: {
    projectId: "p1",
    backlogs: [
      { ...backlog, id: "b2", name: "Icebox", dueOn: null, position: 0 },
      { ...backlog, id: "b1", name: "Sprint 1", dueOn: "2026-08-15T00:00:00Z", position: 1 },
    ],
    sort: "dueOn",
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "List" }));
    const names = canvas
      .getAllByRole("link", { name: /^(Sprint 1|Icebox)$/ })
      .map((el) => el.textContent);
    await expect(names).toEqual(["Sprint 1", "Icebox"]);
  },
};

/** A search with no matches (issue #151): distinct wording from the
 *  no-backlogs-at-all empty state, and "Clear filters" is the way back out. */
export const NoSearchMatches: Story = {
  args: {
    projectId: "p1",
    backlogs: [backlog],
  },
  parameters: {
    nextjs: {
      appDirectory: true,
      navigation: { pathname: "/projects/p1/backlogs", query: { q: "nope" } },
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByText('No backlogs match "nope".')).toBeInTheDocument();
  },
};

export const DeleteConfirm: Story = {
  args: { projectId: "p1", backlogs: [backlog] },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "List" }));
    await userEvent.click(canvas.getByRole("button", { name: "Delete" }));
    await expect(
      canvas.getByText("Its tasks will move to Unclassified. Delete this backlog?"),
    ).toBeInTheDocument();
  },
};
