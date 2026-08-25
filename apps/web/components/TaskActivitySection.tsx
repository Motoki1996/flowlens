"use client";

import { useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, ApiToken, TaskComment } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Markdown } from "@/components/Markdown";

function formatTimestamp(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// A locally-generated id for a comment still in flight — distinct from any
// UUID the API could ever return, so a pending row is unambiguous.
function tempId() {
  return `optimistic-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/**
 * AuthorBadge shows which of the three author_kind values posted a comment
 * (issue #105). An agent comment names its token when the caller can
 * resolve it — the token roster is owner-only (getProjectApiTokens), so
 * apiTokens may be empty for a non-owner and the badge falls back to a
 * generic "Agent" label. A human comment only distinguishes "you" from
 * anyone else: resolving another member's username needs the (also
 * owner-only) member roster, which this section doesn't have either.
 */
function AuthorBadge({
  comment,
  currentUserId,
  apiTokens,
}: {
  comment: TaskComment;
  currentUserId: string;
  apiTokens: ApiToken[];
}) {
  if (comment.authorKind === "gitlab") {
    return <Badge variant="outline">GitLab</Badge>;
  }
  if (comment.authorKind === "agent") {
    const token = apiTokens.find((t) => t.id === comment.authorTokenId);
    return <Badge variant="secondary">Agent{token ? ` · ${token.name}` : ""}</Badge>;
  }
  const mine = comment.authorUserId === currentUserId;
  return <Badge variant={mine ? "default" : "secondary"}>{mine ? "You" : "Team member"}</Badge>;
}

/**
 * DeleteCommentButton removes one comment behind an inline confirmation. The
 * section only ever renders this for the caller's own "user" comments
 * (issue #105's completion condition) — the API would reject anyone else's
 * delete regardless (taskcomment.Service.Delete/DeleteForToken), this just
 * keeps a doomed button from appearing.
 */
function DeleteCommentButton({
  commentId,
  onDeleted,
}: {
  commentId: string;
  onDeleted: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/task-comments/${commentId}`, {
        method: "DELETE",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete the comment.");
        setPending(false);
        return;
      }
      onDeleted();
    } catch {
      setPending(false);
    }
  }

  if (confirming) {
    return (
      <div className="flex items-center gap-2">
        {error ? <span className="text-destructive text-xs">{error}</span> : null}
        <Button variant="destructive" size="sm" onClick={handleDelete} disabled={pending}>
          {pending ? "Deleting…" : "Confirm"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => setConfirming(false)} disabled={pending}>
          Cancel
        </Button>
      </div>
    );
  }

  return (
    <Button variant="ghost" size="sm" onClick={() => setConfirming(true)}>
      Delete
    </Button>
  );
}

/**
 * CommentForm posts to a task's activity log. Posting is optimistic, the
 * same policy the Board views' drag-and-drop already uses
 * (issue #105): the comment is appended to the list immediately under a
 * temporary id, then reconciled with the real one on success or rolled back
 * on failure, rather than waiting on the round trip before showing anything.
 */
function CommentForm({
  taskId,
  currentUserId,
  onPosted,
  onConfirmed,
  onRollback,
}: {
  taskId: string;
  currentUserId: string;
  onPosted: (comment: TaskComment) => void;
  onConfirmed: (tempId: string, comment: TaskComment) => void;
  onRollback: (tempId: string) => void;
}) {
  const [body, setBody] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = body.trim();
    if (!trimmed) return;

    const id = tempId();
    const now = new Date().toISOString();
    onPosted({
      id,
      taskId,
      authorUserId: currentUserId,
      authorTokenId: null,
      authorKind: "user",
      body: trimmed,
      createdAt: now,
      updatedAt: now,
    });
    setBody("");
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/comments`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ body: trimmed }),
      });
      if (!res.ok) {
        const errBody = (await res.json().catch(() => null)) as ApiError | null;
        onRollback(id);
        setError(errBody?.error.message ?? "Failed to post the comment.");
        return;
      }
      onConfirmed(id, (await res.json()) as TaskComment);
    } catch {
      onRollback(id);
      setError("Failed to post the comment.");
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-2" aria-label="Post a comment">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <Textarea
        aria-label="Comment"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        placeholder="Report progress, ask a question, or leave a note…"
        rows={3}
      />
      <div className="flex items-center justify-between gap-2">
        <p className="text-muted-foreground text-xs">Markdown supported — pasted URLs become links.</p>
        <Button type="submit" size="sm" disabled={pending || !body.trim()}>
          {pending ? "Posting…" : "Post"}
        </Button>
      </div>
    </form>
  );
}

/**
 * TaskActivitySection is a task's comment log: the "output" side of the
 * AI-facing task record, sitting beside AIContextSection's "input" side on
 * the Task single view (issue #105) — the return path for an agent that has
 * been reading a task's context but had no way to report back what it did.
 * A comment posted here (or by an agent through the token API) also mirrors
 * to the linked GitLab issue as a note (#104); a "gitlab"-authored comment is
 * one GitLab itself posted, mirrored back in.
 */
export function TaskActivitySection({
  taskId,
  comments: initial,
  currentUserId,
  apiTokens,
}: {
  taskId: string;
  comments: TaskComment[];
  currentUserId: string;
  // The project's API tokens, for naming an agent comment's author — empty
  // for a non-owner caller, since issuing/listing tokens is owner-only.
  apiTokens: ApiToken[];
}) {
  const [comments, setComments] = useState(initial);
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());

  return (
    <div className="space-y-6">
      <CommentForm
        taskId={taskId}
        currentUserId={currentUserId}
        onPosted={(comment) => {
          setComments((current) => [...current, comment]);
          setPendingIds((current) => new Set(current).add(comment.id));
        }}
        onConfirmed={(id, comment) => {
          setComments((current) => current.map((c) => (c.id === id ? comment : c)));
          setPendingIds((current) => {
            const next = new Set(current);
            next.delete(id);
            return next;
          });
        }}
        onRollback={(id) => {
          setComments((current) => current.filter((c) => c.id !== id));
          setPendingIds((current) => {
            const next = new Set(current);
            next.delete(id);
            return next;
          });
        }}
      />

      {comments.length === 0 ? (
        <p className="text-muted-foreground text-sm">No activity yet.</p>
      ) : (
        <ul className="space-y-3">
          {comments.map((comment) => {
            const pending = pendingIds.has(comment.id);
            const deletable =
              !pending && comment.authorKind === "user" && comment.authorUserId === currentUserId;
            return (
              <li
                key={comment.id}
                className="border-border rounded-md border px-3 py-2 text-sm"
                aria-busy={pending}
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <AuthorBadge
                      comment={comment}
                      currentUserId={currentUserId}
                      apiTokens={apiTokens}
                    />
                    <span className="text-muted-foreground text-xs">
                      {pending ? "Posting…" : formatTimestamp(comment.createdAt)}
                    </span>
                  </div>
                  {deletable ? (
                    <DeleteCommentButton
                      commentId={comment.id}
                      onDeleted={() =>
                        setComments((current) => current.filter((c) => c.id !== comment.id))
                      }
                    />
                  ) : null}
                </div>
                <Markdown className="text-foreground mt-1">{comment.body}</Markdown>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
