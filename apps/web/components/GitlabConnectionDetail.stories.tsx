import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { GitlabConnectionDetail } from "./GitlabConnectionDetail";
import { API_PUBLIC_URL } from "@/lib/config";
import type { GitlabConnection } from "@/types";

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

const meta = {
  title: "Screens/GitlabConnection",
  component: GitlabConnectionDetail,
} satisfies Meta<typeof GitlabConnectionDetail>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Not connected: no GitLab connection has been saved yet. */
export const NotConnected: Story = {
  args: { projectId: "p1", connection: null },
};

/** Connected: a verified connection, with its test and change actions. */
export const Connected: Story = {
  args: { projectId: "p1", connection },
};

/** Invalid token: the stored connection's token was rejected by GitLab. */
export const TokenInvalid: Story = {
  args: {
    projectId: "p1",
    connection: {
      ...connection,
      verified: false,
      lastVerifyError: "the personal access token was rejected",
    },
  },
};

/** Submitting the connect form with a token GitLab rejects surfaces the API's error message. */
export const SaveRejected: Story = {
  args: { projectId: "p1", connection: null },
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
