import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { http, HttpResponse } from "msw";
import { LinkedGitlabProjectListSection } from "./LinkedGitlabProjectListSection";
import { API_PUBLIC_URL } from "@/lib/config";
import type { LinkedGitlabProject } from "@/types";

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
  title: "Components/LinkedGitlabProjectListSection",
  component: LinkedGitlabProjectListSection,
  parameters: {
    msw: {
      handlers: [
        http.get(`${API_PUBLIC_URL}/api/v1/projects/:projectId/gitlab-connection/available-projects`, () =>
          HttpResponse.json({ projects: [], nextPage: 0 }),
        ),
      ],
    },
  },
} satisfies Meta<typeof LinkedGitlabProjectListSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Not connected: without a connection there is nothing to search, so linking is hidden. */
export const NotConnected: Story = {
  args: { projectId: "p1", links: [], connected: false },
};

/** Connected, no links yet: a verified connection with no linked GitLab projects yet. */
export const NoLinks: Story = {
  args: { projectId: "p1", links: [], connected: true },
};

/** With links: one link per row, each carrying its scope, last sync and webhook
 *  status, and — on the one row that has it — the Default badge marking where a
 *  task with no link of its own is pushed. */
export const WithLinks: Story = {
  args: {
    projectId: "p1",
    connected: true,
    links: [
      makeLink({}),
      makeLink({
        id: "l2",
        pathWithNamespace: "team/api",
        isDefault: false,
        syncScope: "labels",
        syncLabels: ["bug", "needs-triage"],
        webhookStatus: "not_registered",
        webhookRegisteredAt: null,
        lastSyncedAt: null,
      }),
      makeLink({
        id: "l3",
        pathWithNamespace: "team/web",
        isDefault: false,
        webhookStatus: "failed",
        webhookRegisteredAt: null,
        webhookError: "the token's user needs at least the Maintainer role",
      }),
    ],
  },
};
