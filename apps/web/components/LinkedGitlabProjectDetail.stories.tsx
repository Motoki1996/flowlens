import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { LinkedGitlabProjectDetail } from "./LinkedGitlabProjectDetail";
import type { LinkedGitlabProject, SyncRun, WebhookEvent } from "@/types";

function makeLink(overrides: Partial<LinkedGitlabProject>): LinkedGitlabProject {
  return {
    id: "l1",
    gitlabConnectionId: "c1",
    gitlabProjectId: 100,
    pathWithNamespace: "team/flowlens-demo",
    name: "flowlens-demo",
    webUrl: "https://gitlab.example.com/team/flowlens-demo",
    syncScope: "all",
    syncLabels: [],
    isDefault: true,
    initialImportStatus: "completed",
    lastSyncedAt: "2026-01-06T12:00:00Z",
    webhookStatus: "registered",
    webhookRegisteredAt: "2026-01-05T09:05:00Z",
    webhookError: "",
    createdAt: "2026-01-05T09:00:00Z",
    updatedAt: "2026-01-06T12:00:00Z",
    ...overrides,
  };
}

const runs: SyncRun[] = [
  {
    id: "r1",
    linkedGitlabProjectId: "l1",
    kind: "manual_resync",
    status: "succeeded",
    issuesSeen: 12,
    issuesCreated: 2,
    issuesUpdated: 4,
    startedAt: "2026-01-06T12:00:00Z",
    completedAt: "2026-01-06T12:00:20Z",
    errorMessage: "",
    createdAt: "2026-01-06T12:00:00Z",
  },
  {
    id: "r2",
    linkedGitlabProjectId: "l1",
    kind: "initial_import",
    status: "succeeded",
    issuesSeen: 34,
    issuesCreated: 34,
    issuesUpdated: 0,
    startedAt: "2026-01-05T09:05:00Z",
    completedAt: "2026-01-05T09:06:10Z",
    errorMessage: "",
    createdAt: "2026-01-05T09:05:00Z",
  },
];

const events: WebhookEvent[] = [
  {
    id: "e1",
    linkedGitlabProjectId: "l1",
    eventName: "Issue Hook",
    objectKind: "issue",
    gitlabIssueIid: 7,
    status: "processed",
    skipReason: "",
    errorMessage: "",
    receivedAt: "2026-01-06T12:30:00Z",
    processedAt: "2026-01-06T12:30:01Z",
  },
];

const meta = {
  title: "Screens/LinkedGitlabProject",
  component: LinkedGitlabProjectDetail,
} satisfies Meta<typeof LinkedGitlabProjectDetail>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Webhook正常: a healthy link with sync history and a received event. */
export const WebhookRegistered: Story = {
  args: { projectId: "p1", link: makeLink({}), syncRuns: runs, webhookEvents: events },
};

/** Webhook未登録（APP_PUBLIC_URL未設定）: registration was skipped, so only a retry action shows. */
export const WebhookNotRegistered: Story = {
  args: {
    projectId: "p1",
    link: makeLink({ webhookStatus: "not_registered", webhookRegisteredAt: null, lastSyncedAt: null }),
    syncRuns: [],
    webhookEvents: [],
  },
};

/** Webhook登録失敗: GitLab rejected the registration (e.g. token below Maintainer role). */
export const WebhookFailed: Story = {
  args: {
    projectId: "p1",
    link: makeLink({
      webhookStatus: "failed",
      webhookRegisteredAt: null,
      webhookError:
        "gitlab rejected the webhook registration; the personal access token's user needs at least the Maintainer role on this project",
    }),
    syncRuns: runs,
    webhookEvents: [],
  },
};

/** 同期失敗: the most recent run failed, with the error kept in the history. */
export const SyncFailed: Story = {
  args: {
    projectId: "p1",
    link: makeLink({ syncScope: "labels", syncLabels: ["bug"] }),
    syncRuns: [
      {
        ...runs[0],
        status: "failed",
        completedAt: null,
        errorMessage: "gitlab: unexpected status 401",
      },
    ],
    webhookEvents: [
      {
        ...events[0],
        id: "e2",
        status: "failed",
        errorMessage: "gitlab unreachable",
      },
    ],
  },
};
