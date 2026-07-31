import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { LinkedGitlabProject } from "@/types";
import { LinkedGitlabProjectDetail } from "./LinkedGitlabProjectDetail";

const push = vi.fn();
const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, refresh }),
}));

function makeLink(overrides: Partial<LinkedGitlabProject>): LinkedGitlabProject {
  return {
    id: "link-1",
    gitlabConnectionId: "conn-1",
    gitlabProjectId: 42,
    pathWithNamespace: "group/demo",
    name: "demo",
    webUrl: "https://gitlab.example.com/group/demo",
    syncScope: "all",
    syncLabels: [],
    isDefault: true,
    initialImportStatus: "completed",
    lastSyncedAt: "2026-01-01T00:00:00Z",
    webhookStatus: "registered",
    webhookRegisteredAt: "2026-01-01T00:00:00Z",
    webhookError: "",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("LinkedGitlabProjectDetail", () => {
  beforeEach(() => {
    push.mockClear();
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows identity, attributes and its history sections", () => {
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />);
    expect(screen.getByRole("heading", { name: "group/demo" })).toBeInTheDocument();
    expect(screen.getByText("All issues")).toBeInTheDocument();
    expect(screen.getByText("Webhook registered")).toBeInTheDocument();
    expect(screen.getByText("Sync history")).toBeInTheDocument();
    expect(screen.getByText("Webhook events")).toBeInTheDocument();
  });

  it("offers webhook registration only while the webhook is not registered", () => {
    const { rerender } = render(
      <LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />,
    );
    expect(screen.queryByRole("button", { name: "Register webhook" })).not.toBeInTheDocument();

    rerender(
      <LinkedGitlabProjectDetail
        projectId="p1"
        link={makeLink({ webhookStatus: "failed", webhookError: "needs Maintainer" })}
      />,
    );
    expect(screen.getByRole("button", { name: "Register webhook" })).toBeInTheDocument();
    expect(screen.getByText("needs Maintainer")).toBeInTheDocument();
  });

  it("starts a sync when Sync now is clicked", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 202 }));
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />);

    fireEvent.click(screen.getByRole("button", { name: "Sync now" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/linked-gitlab-projects/link-1/sync-runs"),
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shows an inline error when a sync is already running", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "sync_run_in_progress", message: "conflict" } }), {
        status: 409,
      }),
    );
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />);

    fireEvent.click(screen.getByRole("button", { name: "Sync now" }));

    expect(await screen.findByText("A sync is already running.")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("confirms before unlinking, then returns to the connection", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />);

    fireEvent.click(screen.getByRole("button", { name: "Unlink" }));
    expect(fetch).not.toHaveBeenCalled();
    expect(screen.getByText("Unlink this project?")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Confirm unlink" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/projects/p1/gitlab-connection"));
  });
});
