import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { GitlabConnectionSection } from "./GitlabConnectionSection";
import { API_PUBLIC_URL } from "@/lib/config";
import type { GitlabConnection, LinkedGitlabProject } from "@/types";

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

const meta = {
  title: "Components/GitlabConnectionSection",
  component: GitlabConnectionSection,
  parameters: {
    msw: {
      handlers: [
        http.get(`${API_PUBLIC_URL}/api/v1/projects/:projectId/gitlab-connection/available-projects`, () =>
          HttpResponse.json({ projects: [], nextPage: 0 }),
        ),
      ],
    },
  },
} satisfies Meta<typeof GitlabConnectionSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** 未接続: no GitLab connection has been saved yet. */
export const NotConnected: Story = {
  args: { projectId: "p1", connection: null, linkedProjects: [] },
};

/** 接続済み・連携0件: a verified connection with no linked GitLab projects. */
export const ConnectedNoLinks: Story = {
  args: { projectId: "p1", connection, linkedProjects: [] },
};

/** 連携あり・Webhook正常: a linked project whose webhook is registered. */
export const LinkedWebhookRegistered: Story = {
  args: { projectId: "p1", connection, linkedProjects: [makeLink({})] },
};

/** Webhook未登録（APP_PUBLIC_URL未設定）: webhook registration was skipped, no error to show, just a retry action. */
export const WebhookNotRegistered: Story = {
  args: {
    projectId: "p1",
    connection,
    linkedProjects: [
      makeLink({
        id: "l2",
        webhookStatus: "not_registered",
        webhookRegisteredAt: null,
        lastSyncedAt: null,
      }),
    ],
  },
};

/** Webhook registration reached GitLab but failed (e.g. token below Maintainer role). */
export const WebhookFailed: Story = {
  args: {
    projectId: "p1",
    connection,
    linkedProjects: [
      makeLink({
        id: "l3",
        webhookStatus: "failed",
        webhookRegisteredAt: null,
        webhookError:
          "gitlab rejected the webhook registration; the personal access token's user needs at least the Maintainer role on this project",
      }),
    ],
  },
};

/** トークン無効: the stored connection's token was rejected by GitLab. */
export const TokenInvalid: Story = {
  args: {
    projectId: "p1",
    connection: {
      ...connection,
      verified: false,
      lastVerifyError: "the personal access token was rejected",
    },
    linkedProjects: [],
  },
};

/** Submitting the connect form with a token GitLab rejects surfaces the API's error message. */
export const SaveRejected: Story = {
  args: { projectId: "p1", connection: null, linkedProjects: [] },
  parameters: {
    msw: {
      handlers: [
        http.put(`${API_PUBLIC_URL}/api/v1/projects/:projectId/gitlab-connection`, () =>
          HttpResponse.json(
            { error: { code: "invalid_token", message: "the personal access token was rejected" } },
            { status: 422 },
          ),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(canvas.getByLabelText("GitLab URL"), "https://gitlab.example.com");
    await userEvent.type(canvas.getByLabelText("Access token"), "invalid-token");
    await userEvent.click(canvas.getByRole("button", { name: "Save" }));
    await expect(await canvas.findByText("the personal access token was rejected")).toBeInTheDocument();
  },
};
