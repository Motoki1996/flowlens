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
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const meta = {
  title: "Components/BacklogListSection",
  component: BacklogListSection,
} satisfies Meta<typeof BacklogListSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  args: { projectId: "p1", backlogs: [], tasks: [] },
};

export const Default: Story = {
  args: { projectId: "p1", backlogs: [backlog], tasks: [] },
};

/** The Timeline view mode of the same collection, reached from the List/
 *  Timeline toggle. The bars are covered in BacklogTimelineSection's own
 *  stories; this one covers the toggle that gets you there. */
export const Timeline: Story = {
  args: {
    projectId: "p1",
    backlogs: [{ ...backlog, startDate: "2026-08-01T00:00:00Z", dueOn: "2026-08-31T00:00:00Z" }],
    tasks: [],
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Timeline" }));
    await expect(await canvas.findByRole("list", { name: "Bar colours" })).toBeInTheDocument();
  },
};

export const DeleteConfirm: Story = {
  args: { projectId: "p1", backlogs: [backlog], tasks: [] },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Delete" }));
    await expect(canvas.getByText("配下タスクは未分類に移動します。削除しますか？")).toBeInTheDocument();
  },
};
