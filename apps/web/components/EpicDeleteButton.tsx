"use client";

import { useState } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, Epic } from "@/types";
import { Button } from "@/components/ui/button";

/**
 * EpicDeleteButton interposes an inline confirmation before deleting,
 * spelling out that the epic's tasks stay in their backlog rather than being
 * deleted with it — abandoning the epic rung costs nothing, which is the
 * whole point of it being optional.
 *
 * It is shared by the two screens that own the Epic object, the same as
 * BacklogDeleteButton is for a backlog: the collection view's list rows and
 * the single view. Where to go once the epic is gone differs between them, so
 * the caller says, via `onDeleted`.
 */
export function EpicDeleteButton({
  epic,
  onDeleted,
}: {
  epic: Epic;
  onDeleted: () => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/epics/${epic.id}`, {
        method: "DELETE",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete epic.");
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
        {error ? <span className="text-destructive text-xs">{error}</span> : null}
        <span className="text-foreground text-xs">
          Its tasks will stay in their backlog. Delete this epic?
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
