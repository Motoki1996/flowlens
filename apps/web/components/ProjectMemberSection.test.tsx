import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent, waitFor } from "@testing-library/react";
import type { ProjectMember } from "@/types";
import { ProjectMemberSection } from "./ProjectMemberSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

function makeMember(overrides: Partial<ProjectMember> = {}): ProjectMember {
  return {
    userId: "user-1",
    username: "hubot",
    displayName: "Hubot",
    role: "member",
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("ProjectMemberSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("renders a read-only explanation when members is null (not an owner)", () => {
    render(<ProjectMemberSection projectId="p1" members={null} />);
    expect(screen.getByText("Members")).toBeInTheDocument();
    expect(
      screen.getByText("Only the project's owners can view and manage its members."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add member" })).not.toBeInTheDocument();
  });

  it("shows an empty state when there are no members", () => {
    render(<ProjectMemberSection projectId="p1" members={[]} />);
    expect(screen.getByText("No members yet.")).toBeInTheDocument();
  });

  it("lists a member's name, username, role and join date with owner-only controls", () => {
    const member = makeMember({ role: "viewer" });
    render(<ProjectMemberSection projectId="p1" members={[member]} />);
    expect(screen.getByText("Hubot")).toBeInTheDocument();
    expect(screen.getByText(/@hubot/)).toBeInTheDocument();
    expect(screen.getByLabelText("Role for hubot")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument();
  });

  it("adds a member via the form", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(makeMember({ username: "newperson" })), { status: 201 }),
    );
    render(<ProjectMemberSection projectId="p1" members={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    const form = screen.getByRole("form", { name: "Add member" });
    fireEvent.change(within(form).getByLabelText("Username or email"), {
      target: { value: "newperson" },
    });
    fireEvent.click(within(form).getByRole("button", { name: "Add member" }));

    await waitFor(() => expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/projects/p1/members"),
      expect.objectContaining({ method: "POST" }),
    ));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
  });

  it("requires an identifier before adding", () => {
    render(<ProjectMemberSection projectId="p1" members={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    const form = screen.getByRole("form", { name: "Add member" });
    fireEvent.click(within(form).getByRole("button", { name: "Add member" }));
    expect(screen.getByText("Username or email is required.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("requires a confirmation step before removing", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<ProjectMemberSection projectId="p1" members={[makeMember()]} />);

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Confirm remove" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm remove" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/members/user-1"),
      expect.objectContaining({ method: "DELETE" }),
    ));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
  });

  it("shows an inline error when removing the last owner fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          error: { code: "last_owner", message: "the project's owner cannot be demoted or removed" },
        }),
        { status: 400 },
      ),
    );
    render(<ProjectMemberSection projectId="p1" members={[makeMember({ role: "owner" })]} />);

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm remove" }));

    expect(
      await screen.findByText("the project's owner cannot be demoted or removed"),
    ).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });
});
