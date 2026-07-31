import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { SyncRun } from "@/types";
import { SyncRunSection } from "./SyncRunSection";

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
  it("shows a placeholder when the link has no runs yet", () => {
    render(<SyncRunSection runs={[]} />);
    expect(screen.getByText("Sync history")).toBeInTheDocument();
    expect(screen.getByText("No sync runs yet.")).toBeInTheDocument();
  });

  it("shows a running sync run", () => {
    const run = makeRun({ status: "running", completedAt: null, kind: "manual_resync" });
    render(<SyncRunSection runs={[run]} />);
    expect(screen.getByText("Running…")).toBeInTheDocument();
    expect(screen.getByText("Manual resync")).toBeInTheDocument();
  });

  it("shows a succeeded sync run with its counts", () => {
    const run = makeRun({ status: "succeeded", issuesSeen: 5, issuesCreated: 3, issuesUpdated: 2 });
    render(<SyncRunSection runs={[run]} />);
    expect(screen.getByText("Succeeded")).toBeInTheDocument();
    expect(screen.getByText("5 seen, 3 created, 2 updated")).toBeInTheDocument();
  });

  it("shows a failed sync run with its error message", () => {
    const run = makeRun({ status: "failed", errorMessage: "gitlab: unexpected status 401" });
    render(<SyncRunSection runs={[run]} />);
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("gitlab: unexpected status 401")).toBeInTheDocument();
  });
});
