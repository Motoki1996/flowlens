import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { MergeRequest, Task } from "@/types";
import { MergeRequestDetail } from "./MergeRequestDetail";

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

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: null,
    epicId: null,
    title: "Fix the login bug",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
    size: "m",
    designStartedAt: null,
    implementationStartedAt: null,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

describe("MergeRequestDetail", () => {
  it("shows the merge request's identity: number, title and state", () => {
    render(<MergeRequestDetail mergeRequest={makeMergeRequest({})} projectId="p1" />);
    expect(screen.getByText("!12 Fix the bug")).toBeInTheDocument();
    expect(screen.getByText("Opened", { selector: "span" })).toBeInTheDocument();
  });

  it("shows the source and target branches", () => {
    render(
      <MergeRequestDetail
        mergeRequest={makeMergeRequest({ baseBranch: "main", headBranch: "fix-bug" })}
        projectId="p1"
      />,
    );
    expect(screen.getByText("fix-bug")).toBeInTheDocument();
    expect(screen.getByText("main")).toBeInTheDocument();
  });

  it("links out to the merge request on GitLab", () => {
    render(
      <MergeRequestDetail
        mergeRequest={makeMergeRequest({ htmlUrl: "https://gitlab.example.com/g/p/-/merge_requests/12" })}
        projectId="p1"
      />,
    );
    expect(screen.getByRole("link", { name: "View on GitLab" })).toHaveAttribute(
      "href",
      "https://gitlab.example.com/g/p/-/merge_requests/12",
    );
  });

  it("shows the linked task when one references this merge request", () => {
    render(
      <MergeRequestDetail
        mergeRequest={makeMergeRequest({ taskId: "t1" })}
        projectId="p1"
        task={makeTask({ id: "t1", title: "Fix the login bug" })}
      />,
    );
    expect(screen.getByRole("link", { name: "Fix the login bug" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks/t1",
    );
  });

  it("says no task is linked when the merge request references none", () => {
    render(<MergeRequestDetail mergeRequest={makeMergeRequest({ taskId: null })} projectId="p1" />);
    expect(screen.getByText(/no task linked/i)).toBeInTheDocument();
  });

  it("shows the pipeline status when one is recorded", () => {
    render(
      <MergeRequestDetail mergeRequest={makeMergeRequest({ pipelineStatus: "success" })} projectId="p1" />,
    );
    expect(screen.getByText("Passed")).toBeInTheDocument();
  });

  it("says no pipeline is recorded when there is none", () => {
    render(<MergeRequestDetail mergeRequest={makeMergeRequest({ pipelineStatus: "" })} projectId="p1" />);
    expect(screen.getByText("No pipeline recorded")).toBeInTheDocument();
  });
});
