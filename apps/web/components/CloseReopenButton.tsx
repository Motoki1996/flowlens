"use client";

import { useState } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, Status } from "@/types";
import { Button } from "@/components/ui/button";

/**
 * CloseReopenButton toggles a backlog or an epic between open and closed in
 * place, the same control TaskDetail carries for a task.
 *
 * Unlike a task's, neither object's close touches GitLab — a backlog and an
 * epic have no counterpart there — and neither cascades: the epics and tasks
 * inside stay exactly as they were. That is why there is no confirmation step
 * here. Closing is a visibility decision (a closed backlog drops out of the
 * collection's default listing) and is undone by pressing the button again,
 * so an accidental close costs one click, not a pile of wrongly-closed issues.
 *
 * The object's leftover open work is deliberately not touched or counted here:
 * it is moved to another backlog, or left where it is, by a human — see the
 * 000036 migration.
 */
export function CloseReopenButton<T extends { id: string; status: Status }>({
  object,
  resource,
  noun,
  onChanged,
}: {
  object: T;
  /** The API collection this object lives under, which is also its route
   *  segment: POST /api/v1/{resource}/{id}/close. */
  resource: "backlogs" | "epics";
  /** How the object is named in the error message ("backlog", "epic"). */
  noun: string;
  onChanged: (next: T) => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const action = object.status === "open" ? "close" : "reopen";

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/${resource}/${object.id}/${action}`,
        { method: "POST", credentials: "include", headers: csrfHeaders() },
      );
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? `Failed to ${action} ${noun}.`);
        return;
      }
      onChanged((await res.json()) as T);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col items-end gap-1">
      {error ? <span className="text-destructive text-xs">{error}</span> : null}
      <Button
        variant="outline"
        size="sm"
        onClick={handleClick}
        disabled={pending}
      >
        {action === "close"
          ? pending
            ? "Closing…"
            : `Close ${noun}`
          : pending
            ? "Reopening…"
            : `Reopen ${noun}`}
      </Button>
    </div>
  );
}
