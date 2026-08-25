import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import type { MergeRequest, Task } from "@/types";
import { MergeRequestDetail } from "./MergeRequestDetail";

function makeMergeRequest(overrides: Partial<MergeRequest>): MergeRequest {
  return {
    id: "mr1",
    repositoryId: "r1",
    gitlabMergeRequestId: 100,
    number: 12,
    title: "Fix the login bug",
    state: "opened",
    isDraft: false,
    authorGitlabUsername: "octocat",
    authorAvatarUrl: "",
    baseBranch: "main",
    headBranch: "12-fix-login-bug",
    additions: 42,
    deletions: 7,
    changedFiles: 5,
    gitlabCreatedAt: "2026-01-01T00:00:00Z",
    gitlabUpdatedAt: "2026-01-03T00:00:00Z",
    mergedAt: null,
    closedAt: null,
    htmlUrl: "https://gitlab.example.com/group/demo/-/merge_requests/12",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-03T00:00:00Z",
    firstReviewedAt: "2026-01-02T00:00:00Z",
    pipelineStatus: "success",
    pipelineId: 555,
    pipelineUpdatedAt: "2026-01-03T00:00:00Z",
    taskId: "t1",
    ...overrides,
  };
}

const task: Task = {
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
  progress: "in_progress",
  size: "m",
  designStartedAt: null,
  implementationStartedAt: null,
  createdByUserId: "u1",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
  gitlab: null,
  aiContext: { acceptanceCriteria: "", aiContext: "", updatedAt: null },
};

const meta = {
  title: "Screens/MergeRequest",
  component: MergeRequestDetail,
  args: { mergeRequest: makeMergeRequest({}), projectId: "p1", task },
} satisfies Meta<typeof MergeRequestDetail>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

/** A draft merge request with no pipeline recorded yet and no linked task. */
export const DraftUnlinked: Story = {
  args: {
    mergeRequest: makeMergeRequest({
      isDraft: true,
      pipelineStatus: "",
      pipelineId: null,
      firstReviewedAt: null,
      taskId: null,
    }),
    task: null,
  },
};

/** A merged merge request whose pipeline failed before it merged. */
export const Merged: Story = {
  args: {
    mergeRequest: makeMergeRequest({
      state: "merged",
      mergedAt: "2026-01-05T00:00:00Z",
      pipelineStatus: "failed",
    }),
  },
};
