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

export const DeleteConfirm: Story = {
  args: { projectId: "p1", backlogs: [backlog], tasks: [] },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Delete" }));
    await expect(canvas.getByText("配下タスクは未分類に移動します。削除しますか？")).toBeInTheDocument();
  },
};
