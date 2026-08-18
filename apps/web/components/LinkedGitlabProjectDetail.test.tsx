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

  it("changes the sync scope, sending the scope and its labels together", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(makeLink({})), { status: 200 }));
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByLabelText("Only issues with specific labels"));
    fireEvent.change(screen.getByLabelText("Labels to sync"), {
      target: { value: "bug, needs-triage" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    const [url, init] = vi.mocked(fetch).mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/linked-gitlab-projects/link-1");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(init.body as string)).toEqual({
      syncScope: "labels",
      syncLabels: ["bug", "needs-triage"],
      isDefault: true,
    });
  });

  it("rejects label scope with no labels without calling the API", () => {
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({})} />);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByLabelText("Only issues with specific labels"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(screen.getByText("Enter at least one label to sync by label.")).toBeInTheDocument();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("promotes a non-default link to be its connection's default", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(makeLink({ isDefault: true })), { status: 200 }),
    );
    render(
      <LinkedGitlabProjectDetail
        projectId="p1"
        link={makeLink({ isDefault: false, syncScope: "labels", syncLabels: ["bug"] })}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Set as default" }));

    await waitFor(() => expect(refresh).toHaveBeenCalled());
    const [, init] = vi.mocked(fetch).mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({
      syncScope: "labels",
      syncLabels: ["bug"],
      isDefault: true,
    });
  });

  it("shows a Default badge instead of the action when the link already is the default", () => {
    render(<LinkedGitlabProjectDetail projectId="p1" link={makeLink({ isDefault: true })} />);
    expect(screen.getByText("Default")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Set as default" })).not.toBeInTheDocument();
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
