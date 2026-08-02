import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, screen, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { ApiTokenSection } from "./ApiTokenSection";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiToken } from "@/types";

const readToken: ApiToken = {
  id: "token-read",
  projectId: "p1",
  name: "Read-only agent",
  scopes: ["read"],
  tokenPrefix: "flt_a1b2c3d4",
  lastUsedAt: "2026-02-01T09:00:00Z",
  expiresAt: null,
  createdAt: "2026-01-01T00:00:00Z",
};

const writeToken: ApiToken = {
  id: "token-write",
  projectId: "p1",
  name: "CI writer",
  scopes: ["read", "write"],
  tokenPrefix: "flt_e5f6g7h8",
  lastUsedAt: null,
  expiresAt: "2027-06-01T00:00:00Z",
  createdAt: "2026-01-10T00:00:00Z",
};

const expiringSoonToken: ApiToken = {
  id: "token-expiring",
  projectId: "p1",
  name: "Old integration",
  scopes: ["read"],
  tokenPrefix: "flt_z9y8x7w6",
  lastUsedAt: "2026-07-28T00:00:00Z",
  expiresAt: new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString(),
  createdAt: "2025-08-01T00:00:00Z",
};

const meta = {
  title: "Components/ApiTokenSection",
  component: ApiTokenSection,
} satisfies Meta<typeof ApiTokenSection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Empty: no tokens issued yet — the section explains what the feature is for. */
export const Empty: Story = {
  args: { projectId: "p1", tokens: [] },
};

/** Default: a mix of read-only, read & write, and a token expiring soon. */
export const Default: Story = {
  args: { projectId: "p1", tokens: [readToken, writeToken, expiringSoonToken] },
};

/** IssuedModal: right after issuing, the raw token is shown exactly once. */
export const IssuedModal: Story = {
  args: { projectId: "p1", tokens: [readToken] },
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/api/v1/projects/:projectId/api-tokens`, () =>
          HttpResponse.json(
            {
              id: "token-new",
              projectId: "p1",
              name: "New agent",
              scopes: ["read"],
              tokenPrefix: "flt_newtoken",
              lastUsedAt: null,
              expiresAt: null,
              createdAt: "2026-08-02T00:00:00Z",
              token: "flt_newtoken1234567890",
            },
            { status: 201 },
          ),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "Issue token" }));
    await userEvent.type(canvas.getByLabelText("Name"), "New agent");
    await userEvent.click(canvas.getByRole("button", { name: "Issue token" }));
    // The modal renders in a Radix portal outside canvasElement, so it's
    // queried from the document via `screen` rather than `canvas`.
    await expect(await screen.findByText("flt_newtoken1234567890")).toBeInTheDocument();
    await expect(screen.getByText("Token issued")).toBeInTheDocument();
  },
};
