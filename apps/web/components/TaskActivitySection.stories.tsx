import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse } from "msw";
import { TaskActivitySection } from "./TaskActivitySection";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiToken, TaskComment } from "@/types";

function makeComment(overrides: Partial<TaskComment>): TaskComment {
  return {
    id: "c1",
    taskId: "t1",
    authorUserId: "u1",
    authorTokenId: null,
    authorKind: "user",
    body: "Looks good to me.",
    createdAt: "2026-01-01T09:00:00Z",
    updatedAt: "2026-01-01T09:00:00Z",
    ...overrides,
  };
}

const ciToken: ApiToken = {
  id: "tok1",
  projectId: "p1",
  name: "CI bot",
  scopes: ["read", "write"],
  tokenPrefix: "flt_9f3a",
  lastUsedAt: "2026-01-05T00:00:00Z",
  expiresAt: null,
  createdAt: "2026-01-01T00:00:00Z",
};

const meta = {
  title: "Components/TaskActivitySection",
  component: TaskActivitySection,
} satisfies Meta<typeof TaskActivitySection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** Empty: a new task with no comments yet, next to the post form. */
export const Empty: Story = {
  args: { taskId: "t1", comments: [], currentUserId: "u1", apiTokens: [] },
};

/** HumanOnly: two people discussing the task; only the caller's own comment
 *  gets a delete button. */
export const HumanOnly: Story = {
  args: {
    taskId: "t1",
    comments: [
      makeComment({ id: "c1", authorUserId: "u1", body: "I'll pick this up today." }),
      makeComment({
        id: "c2",
        authorUserId: "u2",
        body: "Thanks — ping me if you get stuck on the migration.",
        createdAt: "2026-01-01T10:30:00Z",
        updatedAt: "2026-01-01T10:30:00Z",
      }),
    ],
    currentUserId: "u1",
    apiTokens: [],
  },
};

/** AgentMixed: a human note, an AI agent's report back (naming its token),
 *  and a comment mirrored in from a GitLab discussion — the three
 *  author_kind values side by side. */
export const AgentMixed: Story = {
  args: {
    taskId: "t1",
    comments: [
      makeComment({ id: "c1", authorUserId: "u1", body: "Assigning this to the agent." }),
      makeComment({
        id: "c2",
        authorUserId: null,
        authorTokenId: "tok1",
        authorKind: "agent",
        body: "Pushed a fix in MR !12; CI is green.",
        createdAt: "2026-01-01T11:00:00Z",
        updatedAt: "2026-01-01T11:00:00Z",
      }),
      makeComment({
        id: "c3",
        authorUserId: null,
        authorTokenId: null,
        authorKind: "gitlab",
        body: "Looks good, merging.",
        createdAt: "2026-01-01T12:00:00Z",
        updatedAt: "2026-01-01T12:00:00Z",
      }),
    ],
    currentUserId: "u1",
    apiTokens: [ciToken],
  },
};

/** Forbidden: a viewer can read the log (posting is member-and-up only), so
 *  the API rejects a post attempt with 403 and the form shows it inline. */
export const Forbidden: Story = {
  args: {
    taskId: "t1",
    comments: [makeComment({ id: "c1", authorUserId: "u2", body: "Already scoped this out." })],
    currentUserId: "u1",
    apiTokens: [],
  },
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/api/v1/tasks/:taskId/comments`, () =>
          HttpResponse.json(
            { error: { code: "forbidden", message: "you don't have permission to comment" } },
            { status: 403 },
          ),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(canvas.getByLabelText("Comment"), "Let me help.");
    await userEvent.click(canvas.getByRole("button", { name: "Post" }));
    await expect(
      await canvas.findByText("you don't have permission to comment"),
    ).toBeInTheDocument();
    // The rejected post is rolled back, not left dangling in the log.
    await expect(canvas.queryByText("Let me help.")).not.toBeInTheDocument();
  },
};
