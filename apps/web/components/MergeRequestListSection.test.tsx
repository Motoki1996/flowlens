import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { MergeRequest } from "@/types";
import { MergeRequestListSection } from "./MergeRequestListSection";

const push = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
  usePathname: () => "/projects/p1/merge-requests",
  useSearchParams: () => new URLSearchParams(),
}));

function makeMergeRequest(overrides: Partial<MergeRequest>): MergeRequest {
  return {
    id: "mr1",
    repositoryId: "r1",
    gitlabMergeRequestId: 100,
    number: 12,
    title: "Fix the bug",
    state: "opened",
    isDraft: false,
    authorGitlabUsername: "octocat",
    authorAvatarUrl: "",
    baseBranch: "main",
    headBranch: "12-fix-the-bug",
    additions: 10,
    deletions: 2,
    changedFiles: 3,
    gitlabCreatedAt: "2026-01-01T00:00:00Z",
    gitlabUpdatedAt: "2026-01-02T00:00:00Z",
    mergedAt: null,
    closedAt: null,
    htmlUrl: "https://gitlab.example.com/group/demo/-/merge_requests/12",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-02T00:00:00Z",
    firstReviewedAt: null,
    pipelineStatus: "",
    pipelineId: null,
    pipelineUpdatedAt: null,
    taskId: null,
    ...overrides,
  };
}

describe("MergeRequestListSection", () => {
  beforeEach(() => {
    push.mockClear();
  });

  it("shows an empty state with zero merge requests", () => {
    render(<MergeRequestListSection projectId="p1" mergeRequests={[]} />);
    expect(screen.getByText(/no merge requests match/i)).toBeInTheDocument();
  });

  it("shows an error message when loading failed", () => {
    render(<MergeRequestListSection projectId="p1" mergeRequests={[]} error />);
    expect(screen.getByText(/failed to load merge requests/i)).toBeInTheDocument();
  });

  it("lists each merge request with its number, title, author and state", () => {
    render(
      <MergeRequestListSection
        projectId="p1"
        mergeRequests={[makeMergeRequest({ number: 12, title: "Fix the bug", authorGitlabUsername: "octocat" })]}
      />,
    );
    expect(screen.getByText("!12 Fix the bug")).toBeInTheDocument();
    expect(screen.getByText(/octocat/)).toBeInTheDocument();
    expect(screen.getByText("Opened")).toBeInTheDocument();
  });

  it("links each row to the merge request's single view", () => {
    render(
      <MergeRequestListSection projectId="p1" mergeRequests={[makeMergeRequest({ id: "mr42" })]} />,
    );
    expect(screen.getByRole("link")).toHaveAttribute("href", "/projects/p1/merge-requests/mr42");
  });

  it("marks a draft merge request", () => {
    render(<MergeRequestListSection projectId="p1" mergeRequests={[makeMergeRequest({ isDraft: true })]} />);
    expect(screen.getByText("Draft")).toBeInTheDocument();
  });

  it("pushes a new state query when the filter changes", async () => {
    render(<MergeRequestListSection projectId="p1" mergeRequests={[]} />);
    fireEvent.click(screen.getByRole("combobox", { name: "State" }));
    fireEvent.click(await screen.findByRole("option", { name: "Merged" }));
    expect(push).toHaveBeenCalledWith("/projects/p1/merge-requests?state=merged");
  });
});
