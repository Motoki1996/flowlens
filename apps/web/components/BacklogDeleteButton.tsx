"use client";

import { useState } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, Backlog } from "@/types";
import { Button } from "@/components/ui/button";

/**
 * BacklogDeleteButton interposes an inline confirmation before deleting,
 * spelling out that the backlog's tasks move to Unclassified rather than being
 * deleted with it.
 *
 * It is shared by the two screens that own the Backlog object, the same as
 * BacklogEditForm: the collection view's list rows and the single view. Where
 * to go once the backlog is gone differs between them — a row just disappears,
 * a single view has nothing left to show — so the caller says, via `onDeleted`.
 */
export function BacklogDeleteButton({
  backlog,
  onDeleted,
}: {
  backlog: Backlog;
  onDeleted: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/backlogs/${backlog.id}`,
        {
          method: "DELETE",
          credentials: "include",
          headers: csrfHeaders(),
        },
      );
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete backlog.");
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
      <div className="flex flex-col items-end gap-1">
        {error ? (
          <span className="text-destructive text-xs">{error}</span>
        ) : null}
        <span className="text-foreground text-xs">
          Its tasks will move to Unclassified. Delete this backlog?
        </span>
        <div className="flex gap-2">
          <Button
            variant="destructive"
            size="sm"
            onClick={handleDelete}
            disabled={pending}
          >
            {pending ? "Deleting…" : "Confirm delete"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setConfirming(false)}
            disabled={pending}
          >
            Cancel
          </Button>
        </div>
      </div>
    );
  }

  return (
    <Button variant="destructive" size="sm" onClick={() => setConfirming(true)}>
      Delete
    </Button>
  );
}
