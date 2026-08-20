import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent, waitFor } from "@testing-library/react";
import type { ProjectInvite, ProjectInviteWithToken } from "@/types";
import { ProjectInviteSection } from "./ProjectInviteSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

function makeInvite(overrides: Partial<ProjectInvite> = {}): ProjectInvite {
  return {
    id: "invite-1",
    projectId: "p1",
    role: "member",
    tokenPrefix: "fli_a1b2c3d4",
    status: "pending",
    expiresAt: "2027-01-01T00:00:00Z",
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("ProjectInviteSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  // The listing endpoint is owner-only, so a non-owner gets null — and a
  // card whose every control is unusable is worse than no card.
  it("renders nothing for a non-owner", () => {
    const { container } = render(<ProjectInviteSection projectId="p1" invites={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("explains what an invite is for when none exist yet", () => {
    render(<ProjectInviteSection projectId="p1" invites={[]} />);
    expect(screen.getByText("Invites")).toBeInTheDocument();
    expect(screen.getByText(/while registration is closed/)).toBeInTheDocument();
    expect(screen.getByText("No invites created yet.")).toBeInTheDocument();
  });

  it("shows the invite link exactly once, built from this app's own origin", async () => {
    const created: ProjectInviteWithToken = { ...makeInvite(), token: "fli_secretvalue" };
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(created), { status: 201 }));
    render(<ProjectInviteSection projectId="p1" invites={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Create invite" }));
    const form = screen.getByRole("form", { name: "Create invite" });
    fireEvent.click(within(form).getByRole("button", { name: "Create invite" }));

    expect(await screen.findByText(`${window.location.origin}/invites/fli_secretvalue`)).toBeInTheDocument();
    expect(screen.getByText(/will not be shown again/)).toBeInTheDocument();
  });

  it("creates the invite with the chosen role and expiry", async () => {
    const created: ProjectInviteWithToken = { ...makeInvite({ role: "viewer" }), token: "fli_x" };
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(created), { status: 201 }));
    render(<ProjectInviteSection projectId="p1" invites={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Create invite" }));
    const form = screen.getByRole("form", { name: "Create invite" });
    fireEvent.click(within(form).getByLabelText("viewer"));
    fireEvent.change(within(form).getByLabelText("Expires in (days)"), { target: { value: "14" } });
    fireEvent.click(within(form).getByRole("button", { name: "Create invite" }));

    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/projects/p1/invites",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ role: "viewer", expiresInDays: 14 }),
        }),
      ),
    );
  });

  it("surfaces the API's message when creating fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "forbidden", message: "insufficient project role" } }), {
        status: 403,
      }),
    );
    render(<ProjectInviteSection projectId="p1" invites={[]} />);

    fireEvent.click(screen.getByRole("button", { name: "Create invite" }));
    const form = screen.getByRole("form", { name: "Create invite" });
    fireEvent.click(within(form).getByRole("button", { name: "Create invite" }));

    expect(await screen.findByText("insufficient project role")).toBeInTheDocument();
  });

  it("lists a pending invite with its prefix and revoke action", () => {
    render(<ProjectInviteSection projectId="p1" invites={[makeInvite()]} />);
    expect(screen.getByText("fli_a1b2c3d4…")).toBeInTheDocument();
    expect(screen.getByText("Pending")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeInTheDocument();
  });

  // A spent or expired invite keeps its row (an owner auditing who was let
  // in needs it) but has nothing left to revoke.
  it("keeps accepted and expired invites listed, without a revoke action", () => {
    render(
      <ProjectInviteSection
        projectId="p1"
        invites={[
          makeInvite({ id: "a", status: "accepted", acceptedAt: "2026-02-01T00:00:00Z" }),
          makeInvite({ id: "b", status: "expired" }),
        ]}
      />,
    );
    expect(screen.getByText("Accepted")).toBeInTheDocument();
    expect(screen.getByText("Expired")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Revoke" })).not.toBeInTheDocument();
  });

  it("confirms before revoking, then calls the API", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<ProjectInviteSection projectId="p1" invites={[makeInvite()]} />);

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    expect(fetch).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));
    await waitFor(() =>
      expect(fetch).toHaveBeenCalledWith(
        "/api/v1/invites/invite-1",
        expect.objectContaining({ method: "DELETE" }),
      ),
    );
    expect(refresh).toHaveBeenCalled();
  });
});
