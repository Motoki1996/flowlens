"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { backlogPath, tasksPath } from "@/lib/routes";
import type { ApiError, Backlog, Task } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

function taskCount(tasks: Task[], backlogId: string) {
  return tasks.filter((t) => t.backlogId === backlogId).length;
}

/** NewBacklogForm is the inline creation form shown in the backlog list. */
function NewBacklogForm({ projectId, onCancel }: { projectId: string; onCancel: () => void }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setError("Backlog name is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/backlogs`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, description }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to create backlog.");
        return;
      }
      router.refresh();
      onCancel();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="New backlog">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="new-backlog-name" className="text-foreground block text-sm font-medium">
          Name
        </label>
        <Input
          id="new-backlog-name"
          name="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label htmlFor="new-backlog-description" className="text-foreground block text-sm font-medium">
          Description
        </label>
        <Textarea
          id="new-backlog-description"
          name="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Creating…" : "Create backlog"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** EditBacklogForm is the inline rename form shown in place of one backlog row. */
function EditBacklogForm({
  backlog,
  onSaved,
  onCancel,
}: {
  backlog: Backlog;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const router = useRouter();
  const [name, setName] = useState(backlog.name);
  const [description, setDescription] = useState(backlog.description);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setError("Backlog name is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/backlogs/${backlog.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, description, position: backlog.position }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to update backlog.");
        return;
      }
      router.refresh();
      onSaved();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label={`Rename ${backlog.name}`}>
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor={`edit-backlog-name-${backlog.id}`} className="text-foreground block text-sm font-medium">
          Name
        </label>
        <Input
          id={`edit-backlog-name-${backlog.id}`}
          name="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label
          htmlFor={`edit-backlog-description-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Description
        </label>
        <Textarea
          id={`edit-backlog-description-${backlog.id}`}
          name="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Saving…" : "Save"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/**
 * DeleteBacklogButton interposes an inline confirmation before deleting,
 * spelling out that the backlog's tasks move to 未分類 rather than being
 * deleted with it.
 */
function DeleteBacklogButton({ backlog }: { backlog: Backlog }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/backlogs/${backlog.id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete backlog.");
        setPending(false);
        return;
      }
      router.refresh();
    } catch {
      setPending(false);
    }
  }

  if (confirming) {
    return (
      <div className="flex flex-col items-end gap-1">
        {error ? <span className="text-destructive text-xs">{error}</span> : null}
        <span className="text-foreground text-xs">配下タスクは未分類に移動します。削除しますか？</span>
        <div className="flex gap-2">
          <Button variant="destructive" size="sm" onClick={handleDelete} disabled={pending}>
            {pending ? "Deleting…" : "Confirm delete"}
          </Button>
          <Button variant="outline" size="sm" onClick={() => setConfirming(false)} disabled={pending}>
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

/**
 * BacklogListSection is the Backlog collection view at
 * /projects/[projectId]/backlogs. Backlog creation, rename and delete all
 * happen here rather than on a separate backlog-management screen — actions
 * live on the object they act on (docs/ui-design.md rule 4).
 */
export function BacklogListSection({
  projectId,
  backlogs,
  tasks = [],
}: {
  projectId: string;
  backlogs: Backlog[];
  tasks?: Task[];
}) {
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Backlogs</CardTitle>
          {!creating ? (
            <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
              New backlog
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {creating ? (
          <div className="mb-4">
            <NewBacklogForm projectId={projectId} onCancel={() => setCreating(false)} />
          </div>
        ) : null}
        {backlogs.length === 0 ? (
          <p className="text-muted-foreground text-sm">No backlogs yet.</p>
        ) : (
          <ul className="space-y-2">
            {backlogs.map((backlog) => (
              <li key={backlog.id} className="border-border rounded-md border px-3 py-2">
                {editingId === backlog.id ? (
                  <EditBacklogForm
                    backlog={backlog}
                    onSaved={() => setEditingId(null)}
                    onCancel={() => setEditingId(null)}
                  />
                ) : (
                  <div className="flex items-center justify-between gap-4">
                    <Link
                      href={backlogPath(projectId, backlog.id)}
                      className="text-foreground text-sm hover:underline"
                    >
                      {backlog.name}{" "}
                      <span className="text-muted-foreground text-xs">
                        ({taskCount(tasks, backlog.id)})
                      </span>
                    </Link>
                    <div className="flex shrink-0 items-center gap-2">
                      {/* Tasks live in the Task collection, filtered — this row
                          hands off to it instead of the list growing a second
                          place to browse tasks (docs/ui-design.md rule 5). */}
                      <Link
                        href={tasksPath(projectId, { backlogId: backlog.id })}
                        className="text-muted-foreground hover:text-foreground text-sm hover:underline"
                      >
                        View tasks
                      </Link>
                      <Button variant="outline" size="sm" onClick={() => setEditingId(backlog.id)}>
                        Rename
                      </Button>
                      <DeleteBacklogButton backlog={backlog} />
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
