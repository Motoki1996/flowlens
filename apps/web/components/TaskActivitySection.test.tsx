import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { ApiToken, TaskComment } from "@/types";
import { TaskActivitySection } from "./TaskActivitySection";

function makeComment(overrides: Partial<TaskComment>): TaskComment {
  return {
    id: "c1",
    taskId: "t1",
    authorUserId: "u1",
    authorTokenId: null,
    authorKind: "user",
    body: "Looks good.",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const token: ApiToken = {
  id: "tok1",
  projectId: "p1",
  name: "CI bot",
  scopes: ["read", "write"],
  tokenPrefix: "flt_9f3a",
  lastUsedAt: null,
  expiresAt: null,
  createdAt: "2026-01-01T00:00:00Z",
};

describe("TaskActivitySection", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn());
  });

  it("shows a placeholder when there is no activity yet", () => {
    render(
      <TaskActivitySection taskId="t1" comments={[]} currentUserId="u1" apiTokens={[]} />,
    );
    expect(screen.getByText("No activity yet.")).toBeInTheDocument();
  });

  it("badges a human, an agent (naming its token), and a GitLab-mirrored comment", () => {
    render(
      <TaskActivitySection
        taskId="t1"
        comments={[
          makeComment({ id: "c1", authorUserId: "u1", authorKind: "user" }),
          makeComment({ id: "c2", authorUserId: "u2", authorKind: "user", body: "Someone else." }),
          makeComment({
            id: "c3",
            authorUserId: null,
            authorTokenId: "tok1",
            authorKind: "agent",
            body: "Pushed a fix.",
          }),
          makeComment({
            id: "c4",
            authorUserId: null,
            authorTokenId: null,
            authorKind: "gitlab",
            body: "Mirrored from GitLab.",
          }),
        ]}
        currentUserId="u1"
        apiTokens={[token]}
      />,
    );
    expect(screen.getByText("You")).toBeInTheDocument();
    expect(screen.getByText("Team member")).toBeInTheDocument();
    expect(screen.getByText("Agent · CI bot")).toBeInTheDocument();
    expect(screen.getByText("GitLab")).toBeInTheDocument();
  });

  it("only shows a delete button for the caller's own user comments", () => {
    render(
      <TaskActivitySection
        taskId="t1"
        comments={[
          makeComment({ id: "c1", authorUserId: "u1", authorKind: "user" }),
          makeComment({ id: "c2", authorUserId: "u2", authorKind: "user" }),
          makeComment({ id: "c3", authorUserId: null, authorTokenId: "tok1", authorKind: "agent" }),
        ]}
        currentUserId="u1"
        apiTokens={[token]}
      />,
    );
    expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(1);
  });

  it("posts a comment optimistically, then reconciles with the server response", async () => {
    const created = makeComment({ id: "server-c1", body: "Shipped it." });
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify(created), { status: 201 }));

    render(
      <TaskActivitySection taskId="t1" comments={[]} currentUserId="u1" apiTokens={[]} />,
    );
    fireEvent.change(screen.getByLabelText("Comment"), { target: { value: "Shipped it." } });
    fireEvent.click(screen.getByRole("button", { name: "Post" }));

    // The comment appears immediately, ahead of the request resolving.
    expect(screen.getByText("Shipped it.")).toBeInTheDocument();
    expect(screen.getAllByText("Posting…").length).toBeGreaterThan(0);

    await waitFor(() => expect(screen.queryAllByText("Posting…")).toHaveLength(0));
    expect(screen.getByText("Shipped it.")).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/tasks/t1/comments",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ body: "Shipped it." }) }),
    );
  });

  it("rolls back the optimistic comment and shows an error when posting fails", async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "invalid_body", message: "body is required" } }), {
        status: 400,
      }),
    );

    render(
      <TaskActivitySection taskId="t1" comments={[]} currentUserId="u1" apiTokens={[]} />,
    );
    fireEvent.change(screen.getByLabelText("Comment"), { target: { value: "Oops." } });
    fireEvent.click(screen.getByRole("button", { name: "Post" }));

    expect(await screen.findByText("body is required")).toBeInTheDocument();
    expect(screen.queryByText("Oops.")).not.toBeInTheDocument();
    expect(screen.getByText("No activity yet.")).toBeInTheDocument();
  });

  it("deletes the caller's own comment", async () => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 204 }));

    render(
      <TaskActivitySection
        taskId="t1"
        comments={[makeComment({ id: "c1", authorUserId: "u1", authorKind: "user" })]}
        currentUserId="u1"
        apiTokens={[]}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(screen.getByText("No activity yet.")).toBeInTheDocument());
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/task-comments/c1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });
});
