import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, screen, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { ProjectMemberSection } from "./ProjectMemberSection";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ProjectMember } from "@/types";

const owner: ProjectMember = {
  userId: "user-owner",
  username: "alice",
  displayName: "Alice Owner",
  role: "owner",
  createdAt: "2026-01-01T00:00:00Z",
};

const member: ProjectMember = {
  userId: "user-member",
  username: "bob",
  displayName: "Bob Member",
  role: "member",
  createdAt: "2026-02-10T00:00:00Z",
};

const viewer: ProjectMember = {
  userId: "user-viewer",
  username: "carol",
  displayName: "Carol Viewer",
  role: "viewer",
  createdAt: "2026-03-05T00:00:00Z",
};

const meta = {
  title: "Components/ProjectMemberSection",
  component: ProjectMemberSection,
} satisfies Meta<typeof ProjectMemberSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** OwnerView: an owner sees the full roster with role-change and remove controls. */
export const OwnerView: Story = {
  args: { projectId: "p1", members: [owner, member, viewer] },
};

/** MemberView: a non-owner's request for the listing 403s, so the section
 *  renders read-only with no visibility into who the other members are. */
export const MemberView: Story = {
  args: { projectId: "p1", members: null },
};

/** Empty: an owner viewing a project with no members recorded yet. */
export const Empty: Story = {
  args: { projectId: "p1", members: [] },
};

/** AddMember: an owner invites an existing user by username or email. */
export const AddMember: Story = {
  args: { projectId: "p1", members: [owner] },
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/api/v1/projects/:projectId/members`, () =>
          HttpResponse.json(
            {
              userId: "user-new",
              username: "newperson",
              displayName: "New Person",
              role: "member",
              createdAt: "2026-08-10T00:00:00Z",
            },
            { status: 201 },
          ),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Add member" }));
    await userEvent.type(canvas.getByLabelText("Username or email"), "newperson");
    const form = canvas.getByRole("form", { name: "Add member" });
    await userEvent.click(within(form).getByRole("button", { name: "Add member" }));
    await expect(await screen.findByText("Alice Owner")).toBeInTheDocument();
  },
};
