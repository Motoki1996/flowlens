import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent, waitFor } from "@testing-library/react";
import type { ProjectMember, ProjectMemberCandidate } from "@/types";
import { ProjectMemberSection } from "./ProjectMemberSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

/** mockFetch routes by URL so the candidate search and the invite POST can
 *  answer differently in one test. */
function mockFetch(routes: {
  candidates?: () => Response | Promise<Response>;
  post?: () => Response;
}) {
  vi.mocked(fetch).mockImplementation(async (input) => {
    const url = String(input);
    if (url.includes("member-candidates")) {
      return routes.candidates ? routes.candidates() : new Response("[]", { status: 200 });
    }
    return routes.post ? routes.post() : new Response(null, { status: 204 });
  });
}

function makeCandidate(overrides: Partial<ProjectMemberCandidate> = {}): ProjectMemberCandidate {
  return { userId: "user-9", username: "hubot", displayName: "Hubot", ...overrides };
}

function makeMember(overrides: Partial<ProjectMember> = {}): ProjectMember {
  return {
    userId: "user-1",
    username: "hubot",
    displayName: "Hubot",
    role: "member",
    isProjectOwner: false,
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
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={null} />);
    expect(screen.getByText("Members")).toBeInTheDocument();
    expect(
      screen.getByText("Only the project's owners can view and manage its members."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add member" })).not.toBeInTheDocument();
  });

  it("shows an empty state when there are no members", () => {
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);
    expect(screen.getByText("No members yet.")).toBeInTheDocument();
  });

  it("lists a member's name, username, role and join date with owner-only controls", () => {
    const member = makeMember({ role: "viewer" });
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[member]} />);
    expect(screen.getByText("Hubot")).toBeInTheDocument();
    expect(screen.getByText(/@hubot/)).toBeInTheDocument();
    expect(screen.getByLabelText("Role for hubot")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument();
  });

  it("shows the viewer's own row as a role badge with no controls", () => {
    render(
      <ProjectMemberSection
        currentUserId="user-1"
        projectId="p1"
        members={[makeMember({ role: "owner" })]}
      />,
    );
    expect(screen.getByText("You")).toBeInTheDocument();
    expect(screen.getByText("Owner")).toBeInTheDocument();
    expect(screen.queryByLabelText("Role for hubot")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove" })).not.toBeInTheDocument();
  });

  it("shows the designated owner's row as a role badge with no controls", () => {
    render(
      <ProjectMemberSection
        currentUserId="me"
        projectId="p1"
        members={[makeMember({ role: "owner", isProjectOwner: true })]}
      />,
    );
    expect(screen.queryByText("You")).not.toBeInTheDocument();
    expect(screen.getByText("Owner")).toBeInTheDocument();
    expect(screen.queryByLabelText("Role for hubot")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove" })).not.toBeInTheDocument();
  });

  it("adds a member via the form", async () => {
    mockFetch({
      post: () =>
        new Response(JSON.stringify(makeMember({ username: "newperson" })), { status: 201 }),
    });
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);

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

  it("suggests matching users and fills the field when one is picked", async () => {
    mockFetch({
      candidates: () => new Response(JSON.stringify([makeCandidate()]), { status: 200 }),
    });
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    const field = screen.getByLabelText("Username or email");
    fireEvent.change(field, { target: { value: "hub" } });

    const option = await screen.findByRole("option", { name: /Hubot/ });
    fireEvent.mouseDown(option);
    expect(field).toHaveValue("hubot");
    // Picking a suggestion closes the list rather than leaving it over the form.
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("does not search until the term is long enough", async () => {
    mockFetch({});
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    fireEvent.change(screen.getByLabelText("Username or email"), { target: { value: "h" } });

    await new Promise((resolve) => setTimeout(resolve, 400));
    expect(fetch).not.toHaveBeenCalled();
  });

  it("still submits a typed identifier that matches no suggestion", async () => {
    mockFetch({
      post: () =>
        new Response(JSON.stringify(makeMember({ username: "stranger" })), { status: 201 }),
    });
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    const form = screen.getByRole("form", { name: "Add member" });
    fireEvent.change(within(form).getByLabelText("Username or email"), {
      target: { value: "stranger@example.com" },
    });
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

    fireEvent.click(within(form).getByRole("button", { name: "Add member" }));
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        expect.stringContaining("/projects/p1/members"),
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ identifier: "stranger@example.com", role: "member" }),
        }),
      ),
    );
  });

  it("reports a failed search without blocking the form", async () => {
    mockFetch({ candidates: () => new Response(null, { status: 500 }) });
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    fireEvent.change(screen.getByLabelText("Username or email"), { target: { value: "hub" } });

    expect(
      await screen.findByText(
        "Couldn't load suggestions — enter an exact username or email instead.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add member" })).toBeEnabled();
  });

  it("requires an identifier before adding", () => {
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[]} />);
    fireEvent.click(screen.getByRole("button", { name: "Add member" }));
    const form = screen.getByRole("form", { name: "Add member" });
    fireEvent.click(within(form).getByRole("button", { name: "Add member" }));
    expect(screen.getByText("Username or email is required.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("requires a confirmation step before removing", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<ProjectMemberSection currentUserId="me" projectId="p1" members={[makeMember()]} />);

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

  it("shows an inline error when the server rejects a removal", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          error: { code: "last_owner", message: "the project's owner cannot be demoted or removed" },
        }),
        { status: 400 },
      ),
    );
    render(
      <ProjectMemberSection
        currentUserId="me"
        projectId="p1"
        members={[makeMember({ role: "owner" })]}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm remove" }));

    expect(
      await screen.findByText("the project's owner cannot be demoted or removed"),
    ).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });
});
