import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { GitlabConnection } from "@/types";
import { GitlabConnectionDetail } from "./GitlabConnectionDetail";

const push = vi.fn();
const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh }),
}));

const connection: GitlabConnection = {
  projectId: "p1",
  baseUrl: "https://gitlab.example.com",
  tokenLastFour: "a1b2",
  tokenGitlabUserId: 42,
  tokenGitlabUsername: "octocat",
  verified: true,
  lastVerifiedAt: "2026-01-05T09:00:00Z",
  lastVerifyError: "",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-05T09:00:00Z",
};

describe("GitlabConnectionDetail", () => {
  beforeEach(() => {
    push.mockClear();
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("confirms before disconnecting, saying what else goes with it", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<GitlabConnectionDetail projectId="p1" connection={connection} />);

    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(
      screen.getByText("Disconnect GitLab? This unlinks every linked project and stops syncing."),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm disconnect" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/projects/p1/gitlab-connection",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("keeps the connection when the disconnect is cancelled", () => {
    render(<GitlabConnectionDetail projectId="p1" connection={connection} />);

    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("button", { name: "Disconnect" })).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("shows an inline error when the disconnect fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "not_found", message: "gitlab connection not found" } }),
        { status: 404 },
      ),
    );
    render(<GitlabConnectionDetail projectId="p1" connection={connection} />);

    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm disconnect" }));

    expect(await screen.findByText("gitlab connection not found")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("offers no disconnect action when there is no connection yet", () => {
    render(<GitlabConnectionDetail projectId="p1" connection={null} />);

    expect(screen.getByRole("form", { name: "Connect GitLab" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Disconnect" })).not.toBeInTheDocument();
  });
});
