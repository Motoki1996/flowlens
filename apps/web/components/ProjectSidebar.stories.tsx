import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { ProjectSidebar } from "./ProjectSidebar";
import type { Project } from "@/types";

function project(id: string, name: string): Project {
  return {
    id,
    name,
    description: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    failedSyncTaskCount: 0,
  };
}

const projects = [project("p1", "Alpha"), project("p2", "Beta")];

const meta = {
  title: "Components/ProjectSidebar",
  component: ProjectSidebar,
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: "/projects/p1/tasks" } },
  },
  args: {
    project: projects[0],
    projects,
    counts: { backlogs: 2, openTasks: 3, totalTasks: 7, gitlab: "1" },
  },
} satisfies Meta<typeof ProjectSidebar>;

export default meta;
type Story = StoryObj<typeof meta>;

/** On a collection screen: that section is the current one. */
export const Default: Story = {};

/** A single view marks the collection above it, not nothing. */
export const OnASingleView: Story = {
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: "/projects/p1/tasks/t1" } },
  },
};

/** The connection is configured but failing to verify. */
export const GitlabConnectionBroken: Story = {
  args: { counts: { backlogs: 2, openTasks: 3, totalTasks: 7, gitlab: "Error" } },
};

/** Counts unavailable: every link still works, they just carry no summary. */
export const CountsUnavailable: Story = {
  args: { counts: { backlogs: null, openTasks: null, totalTasks: null, gitlab: null } },
};
