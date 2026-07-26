import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { AppHeader } from "./AppHeader";
import type { User } from "@/types";

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

const meta = {
  title: "Components/AppHeader",
  component: AppHeader,
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof AppHeader>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { user },
};

export const NoDisplayName: Story = {
  args: { user: { ...user, displayName: "" } },
};
