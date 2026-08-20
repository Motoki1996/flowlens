import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import type { MergeRequest } from "@/types";
import { MergeRequestListSection } from "./MergeRequestListSection";

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

const meta = {
  title: "Screens/MergeRequests",
  component: MergeRequestListSection,
  parameters: {
    nextjs: { appDirectory: true, navigation: { pathname: "/projects/p1/merge-requests" } },
  },
  args: {
    projectId: "p1",
    mergeRequests: [
      makeMergeRequest({
        id: "mr1",
        number: 12,
        title: "Fix the login bug",
        state: "opened",
        pipelineStatus: "success",
      }),
      makeMergeRequest({
        id: "mr2",
        number: 13,
        title: "Add dark mode",
        state: "opened",
        isDraft: true,
        pipelineStatus: "failed",
        authorGitlabUsername: "hubot",
      }),
      makeMergeRequest({
        id: "mr3",
        number: 10,
        title: "Refactor the sync worker",
        state: "merged",
        mergedAt: "2026-01-10T00:00:00Z",
        pipelineStatus: "success",
      }),
    ],
  },
} satisfies Meta<typeof MergeRequestListSection>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Empty: Story = {
  args: { mergeRequests: [] },
};

export const LoadFailed: Story = {
  args: { mergeRequests: [], error: true },
};

/** ?state=merged filters the collection to merged merge requests only. */
export const FilteredByState: Story = {
  args: {
    state: "merged",
    mergeRequests: [
      makeMergeRequest({
        id: "mr3",
        number: 10,
        title: "Refactor the sync worker",
        state: "merged",
        mergedAt: "2026-01-10T00:00:00Z",
      }),
    ],
  },
};

/**
 * A project with more merge requests than fit one page: the pager appears
 * with the range it covers, and "Previous" is disabled on the first page.
 */
export const Paged: Story = {
  args: { page: 1, perPage: 3, nextPage: 2, totalCount: 128 },
};

/** The last page of the same collection — "Next" is disabled. */
export const LastPage: Story = {
  args: { page: 43, perPage: 3, nextPage: 0, totalCount: 128 },
};
