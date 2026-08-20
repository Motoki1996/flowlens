import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ProjectInvitePreview, User } from "@/types";

const getCurrentUser = vi.fn();
const getInvitePreview = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getInvitePreview: (token: string) => getInvitePreview(token),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

import InvitePage from "./page";

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

const preview: ProjectInvitePreview = {
  projectId: "p1",
  projectName: "Alpha",
  role: "member",
  expiresAt: "2027-01-01T00:00:00Z",
};

describe("InvitePage", () => {
  beforeEach(() => {
    getCurrentUser.mockReset();
    getInvitePreview.mockReset();
  });

  // The whole point of issue #211: someone with no account has to be able to
  // get one from here, so the screen must render a signup form rather than
  // bounce to /login.
  it("offers a signup form to a signed-out invitee, naming the project and role", async () => {
    getInvitePreview.mockResolvedValue(preview);
    getCurrentUser.mockResolvedValue(null);

    render(await InvitePage({ params: Promise.resolve({ token: "fli_abc" }) }));

    expect(screen.getByText("Join Alpha")).toBeInTheDocument();
    expect(screen.getByText(/as a member/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create account & join" })).toBeInTheDocument();
  });

  it("offers to join directly when the invitee already has an account", async () => {
    getInvitePreview.mockResolvedValue(preview);
    getCurrentUser.mockResolvedValue(user);

    render(await InvitePage({ params: Promise.resolve({ token: "fli_abc" }) }));

    expect(screen.getByRole("button", { name: "Join project" })).toBeInTheDocument();
    expect(screen.getByText(/Signed in as octocat/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create account & join" })).not.toBeInTheDocument();
  });

  // The API deliberately does not distinguish unknown from expired from
  // already-used, so the screen says the same thing for all three.
  it("reports one indistinguishable message for an unusable invite", async () => {
    getInvitePreview.mockResolvedValue(null);
    getCurrentUser.mockResolvedValue(null);

    render(await InvitePage({ params: Promise.resolve({ token: "fli_bad" }) }));

    expect(screen.getByText("Invite unavailable")).toBeInTheDocument();
    expect(screen.getByText(/invalid, has expired, or has already been used/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create account & join" })).not.toBeInTheDocument();
  });
});
