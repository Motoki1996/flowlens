import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { LinkedGitlabProject, SyncRun } from "@/types";
import { SyncRunSection } from "./SyncRunSection";

const refresh = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ refresh }),
}));

const link: LinkedGitlabProject = {
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
};

function makeRun(overrides: Partial<SyncRun>): SyncRun {
  return {
    id: "run-1",
    linkedGitlabProjectId: "link-1",
    kind: "initial_import",
    status: "succeeded",
    issuesSeen: 0,
    issuesCreated: 0,
    issuesUpdated: 0,
    startedAt: "2026-01-01T00:00:00Z",
    completedAt: "2026-01-01T00:05:00Z",
    errorMessage: "",
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("SyncRunSection", () => {
  beforeEach(() => {
    refresh.mockClear();
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows a placeholder when no GitLab project is linked", () => {
    render(<SyncRunSection linkedProjects={[]} syncRunsByLink={{}} />);
    expect(screen.getByText("Sync history")).toBeInTheDocument();
    expect(screen.getByText("Link a GitLab project to see its sync history here.")).toBeInTheDocument();
  });

  it("shows no history for a linked project with no runs yet", () => {
    render(<SyncRunSection linkedProjects={[link]} syncRunsByLink={{}} />);
    expect(screen.getByText("group/demo")).toBeInTheDocument();
    expect(screen.getByText("No sync runs yet.")).toBeInTheDocument();
  });

  it("shows a running sync run", () => {
    const run = makeRun({ status: "running", completedAt: null, kind: "manual_resync" });
    render(<SyncRunSection linkedProjects={[link]} syncRunsByLink={{ "link-1": [run] }} />);
    expect(screen.getByText("Running…")).toBeInTheDocument();
    expect(screen.getByText("Manual resync")).toBeInTheDocument();
  });

  it("shows a succeeded sync run with its counts", () => {
    const run = makeRun({ status: "succeeded", issuesSeen: 5, issuesCreated: 3, issuesUpdated: 2 });
    render(<SyncRunSection linkedProjects={[link]} syncRunsByLink={{ "link-1": [run] }} />);
    expect(screen.getByText("Succeeded")).toBeInTheDocument();
    expect(screen.getByText("5 seen, 3 created, 2 updated")).toBeInTheDocument();
  });

  it("shows a failed sync run with its error message", () => {
    const run = makeRun({ status: "failed", errorMessage: "gitlab: unexpected status 401" });
    render(<SyncRunSection linkedProjects={[link]} syncRunsByLink={{ "link-1": [run] }} />);
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("gitlab: unexpected status 401")).toBeInTheDocument();
  });

  it("starts a sync when Sync now is clicked", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify(makeRun({ status: "running" })), { status: 202 }),
    );
    render(<SyncRunSection linkedProjects={[link]} syncRunsByLink={{}} />);

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
    render(<SyncRunSection linkedProjects={[link]} syncRunsByLink={{}} />);

    fireEvent.click(screen.getByRole("button", { name: "Sync now" }));

    expect(await screen.findByText("A sync is already running.")).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
  });
});
