import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, screen, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { ProjectMemberSection } from "./ProjectMemberSection";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ProjectMember, ProjectMemberCandidate } from "@/types";

const owner: ProjectMember = {
  userId: "user-owner",
  username: "alice",
  displayName: "Alice Owner",
  role: "owner",
  isProjectOwner: true,
  createdAt: "2026-01-01T00:00:00Z",
};

/** A second owner: the "owner" role without being the designated owner. */
const coOwner: ProjectMember = {
  userId: "user-coowner",
  username: "dave",
  displayName: "Dave Co-owner",
  role: "owner",
  isProjectOwner: false,
  createdAt: "2026-02-01T00:00:00Z",
};

const member: ProjectMember = {
  userId: "user-member",
  username: "bob",
  displayName: "Bob Member",
  role: "member",
  isProjectOwner: false,
  createdAt: "2026-02-10T00:00:00Z",
};

const viewer: ProjectMember = {
  userId: "user-viewer",
  username: "carol",
  displayName: "Carol Viewer",
  role: "viewer",
  isProjectOwner: false,
  createdAt: "2026-03-05T00:00:00Z",
};

const meta = {
  title: "Components/ProjectMemberSection",
  component: ProjectMemberSection,
} satisfies Meta<typeof ProjectMemberSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** OwnerView: the designated owner sees the full roster. Their own row carries
 *  no controls — it is both their row and the undemotable owner's. */
export const OwnerView: Story = {
  args: { projectId: "p1", currentUserId: owner.userId, members: [owner, member, viewer] },
};

/** CoOwnerView: an owner who is not the designated owner. Their own row and
 *  the designated owner's both render as plain role badges (issue #139); only
 *  the rows they can actually act on keep their controls. */
export const CoOwnerView: Story = {
  args: {
    projectId: "p1",
    currentUserId: coOwner.userId,
    members: [owner, coOwner, member, viewer],
  },
};

/** MemberView: a non-owner's request for the listing 403s, so the section
 *  renders read-only with no visibility into who the other members are. */
export const MemberView: Story = {
  args: { projectId: "p1", currentUserId: "user-member", members: null },
};

/** Empty: an owner viewing a project with no members recorded yet. */
export const Empty: Story = {
  args: { projectId: "p1", currentUserId: owner.userId, members: [] },
};

/** candidateHandler answers the invite form's user search with `candidates`,
 *  or fails with `status` when one is given. */
function candidateHandler(candidates: ProjectMemberCandidate[], status = 200) {
  return http.get(`${API_PUBLIC_URL}/api/v1/projects/:projectId/member-candidates`, () =>
    status === 200
      ? HttpResponse.json(candidates)
      : new HttpResponse(null, { status }),
  );
}

const candidate: ProjectMemberCandidate = {
  userId: "user-new",
  username: "newperson",
  displayName: "New Person",
};

/** AddMember: an owner invites an existing user by username or email. */
export const AddMember: Story = {
  args: { projectId: "p1", currentUserId: owner.userId, members: [owner] },
  parameters: {
    msw: {
      handlers: [
        candidateHandler([]),
        http.post(`${API_PUBLIC_URL}/api/v1/projects/:projectId/members`, () =>
          HttpResponse.json(
            {
              userId: "user-new",
              username: "newperson",
              displayName: "New Person",
              role: "member",
              isProjectOwner: false,
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

/** AddMemberWithSuggestions: typing narrows the list of people the owner
 *  already shares a project with; picking one fills the field. */
export const AddMemberWithSuggestions: Story = {
  args: { projectId: "p1", currentUserId: owner.userId, members: [owner] },
  parameters: { msw: { handlers: [candidateHandler([candidate])] } },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Add member" }));
    await userEvent.type(canvas.getByLabelText("Username or email"), "new");
    const option = await canvas.findByRole("option", { name: /New Person/ });
    await userEvent.click(option);
    await expect(canvas.getByLabelText("Username or email")).toHaveValue("newperson");
  },
};

/** AddMemberNoSuggestions: nobody matches — someone outside the owner's
 *  shared projects is still invitable by exact username or email. */
export const AddMemberNoSuggestions: Story = {
  args: { projectId: "p1", currentUserId: owner.userId, members: [owner] },
  parameters: { msw: { handlers: [candidateHandler([])] } },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Add member" }));
    await userEvent.type(canvas.getByLabelText("Username or email"), "stranger@example.com");
    await expect(canvas.queryByRole("listbox")).not.toBeInTheDocument();
  },
};

/** AddMemberSearchFails: the search request fails; the field keeps working as
 *  the plain identifier input it has always been. */
export const AddMemberSearchFails: Story = {
  args: { projectId: "p1", currentUserId: owner.userId, members: [owner] },
  parameters: { msw: { handlers: [candidateHandler([], 500)] } },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Add member" }));
    await userEvent.type(canvas.getByLabelText("Username or email"), "new");
    await expect(
      await canvas.findByText(
        "Couldn't load suggestions — enter an exact username or email instead.",
      ),
    ).toBeInTheDocument();
  },
};
