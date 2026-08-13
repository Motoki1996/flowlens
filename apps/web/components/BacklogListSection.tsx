"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { ChevronDown, ChevronUp, GripVertical, Plus } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { backlogPath, tasksPath } from "@/lib/routes";
import { fromApiDate, toApiDate } from "@/lib/dates";
import { backlogScheduleLabel } from "@/lib/backlogs";
import type { ApiError, Backlog, Priority, Progress } from "@/types";
import { PROGRESS_COLUMNS } from "@/lib/progress";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { DateField } from "@/components/DateField";
import { PriorityBadge } from "@/components/PriorityBadge";
import { ProgressBadge } from "@/components/ProgressBadge";
import { BacklogBoardSection } from "@/components/BacklogBoardSection";
import { ViewModeToggle, type ViewMode } from "@/components/ViewModeToggle";

/**
 * The Timeline view mode pulls in the charting library, which the default List
 * mode has no use for — loading it on demand keeps that cost off the backlog
 * collection until someone actually switches views. Same arrangement as the
 * Task collection.
 */
const BacklogTimelineSection = dynamic(
  () => import("@/components/BacklogTimelineSection").then((m) => m.BacklogTimelineSection),
  { loading: () => <p className="text-muted-foreground text-sm">Loading timeline…</p> },
);

/** moveItem returns a copy of list with the item at fromIndex relocated to
 *  toIndex, used by both drag-and-drop and the up/down move buttons so the
 *  two interactions produce identical orderings. */
function moveItem<T>(list: T[], fromIndex: number, toIndex: number): T[] {
  const next = [...list];
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  return next;
}

/** NewBacklogForm is the inline creation form shown in the backlog list. */
function NewBacklogForm({ projectId, onCancel }: { projectId: string; onCancel: () => void }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [startDate, setStartDate] = useState<Date | undefined>(undefined);
  const [dueOn, setDueOn] = useState<Date | undefined>(undefined);
  const [priority, setPriority] = useState<Priority>("medium");
  const [progress, setProgress] = useState<Progress>("not_started");
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          name,
          description,
          startDate: toApiDate(startDate),
          dueOn: toApiDate(dueOn),
          priority,
          progress,
        }),
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
      <div className="grid gap-3 sm:grid-cols-2">
        <DateField
          id="new-backlog-start-date"
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField id="new-backlog-due-on" label="Due date" value={dueOn} onChange={setDueOn} />
      </div>
      <div>
        <label htmlFor="new-backlog-priority" className="text-foreground block text-sm font-medium">
          Priority
        </label>
        <Select value={priority} onValueChange={(value) => setPriority(value as Priority)}>
          <SelectTrigger id="new-backlog-priority" className="mt-1 w-full sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="low">Low</SelectItem>
            <SelectItem value="medium">Medium</SelectItem>
            <SelectItem value="high">High</SelectItem>
            <SelectItem value="urgent">Urgent</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div>
        <label htmlFor="new-backlog-progress" className="text-foreground block text-sm font-medium">
          Progress
        </label>
        <Select value={progress} onValueChange={(value) => setProgress(value as Progress)}>
          <SelectTrigger id="new-backlog-progress" className="mt-1 w-full sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PROGRESS_COLUMNS.map((option) => (
              <SelectItem key={option.progress} value={option.progress}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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

/** EditBacklogForm is the inline edit form shown in place of one backlog row.
 *  It covers the schedule as well as the name, so the action is "Edit", not
 *  "Rename". */
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
  const [startDate, setStartDate] = useState(fromApiDate(backlog.startDate));
  const [dueOn, setDueOn] = useState(fromApiDate(backlog.dueOn));
  const [priority, setPriority] = useState<Priority>(backlog.priority);
  const [progress, setProgress] = useState<Progress>(backlog.progress);
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          name,
          description,
          position: backlog.position,
          startDate: toApiDate(startDate),
          dueOn: toApiDate(dueOn),
          priority,
          progress,
        }),
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
    <form onSubmit={handleSubmit} className="space-y-3" aria-label={`Edit ${backlog.name}`}>
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
      <div className="grid gap-3 sm:grid-cols-2">
        <DateField
          id={`edit-backlog-start-date-${backlog.id}`}
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField
          id={`edit-backlog-due-on-${backlog.id}`}
          label="Due date"
          value={dueOn}
          onChange={setDueOn}
        />
      </div>
      <div>
        <label
          htmlFor={`edit-backlog-priority-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Priority
        </label>
        <Select value={priority} onValueChange={(value) => setPriority(value as Priority)}>
          <SelectTrigger id={`edit-backlog-priority-${backlog.id}`} className="mt-1 w-full sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="low">Low</SelectItem>
            <SelectItem value="medium">Medium</SelectItem>
            <SelectItem value="high">High</SelectItem>
            <SelectItem value="urgent">Urgent</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div>
        <label
          htmlFor={`edit-backlog-progress-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Progress
        </label>
        <Select value={progress} onValueChange={(value) => setProgress(value as Progress)}>
          <SelectTrigger id={`edit-backlog-progress-${backlog.id}`} className="mt-1 w-full sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PROGRESS_COLUMNS.map((option) => (
              <SelectItem key={option.progress} value={option.progress}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
 * spelling out that the backlog's tasks move to Unclassified rather than being
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
        headers: csrfHeaders(),
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
        <span className="text-foreground text-xs">
          Its tasks will move to Unclassified. Delete this backlog?
        </span>
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
 * /projects/[projectId]/backlogs. List and Timeline are view modes of this one
 * screen (docs/ui-design.md rule 5), and backlog creation, editing and delete
 * all happen here rather than on a separate backlog-management screen —
 * actions live on the object they act on (rule 4).
 */
export function BacklogListSection({
  projectId,
  backlogs,
}: {
  projectId: string;
  backlogs: Backlog[];
}) {
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  // Board is the default: how far along each backlog is, is the first question
  // asked of a backlog collection, and the board answers it without reading
  // every row.
  const [view, setView] = useState<ViewMode>("board");

  // `order` mirrors `backlogs` but is reordered optimistically on drag/move,
  // ahead of the PATCH .../backlogs/order round trip — router.refresh()
  // (used everywhere else in this file) would otherwise force a full
  // server-component re-render per drag, which doesn't read as drag-and-drop
  // at all (issue #79). It resyncs whenever the server data changes under it
  // (e.g. after a create/delete elsewhere on the page).
  const [order, setOrder] = useState(backlogs);
  useEffect(() => setOrder(backlogs), [backlogs]);
  const [reorderError, setReorderError] = useState<string | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);

  async function commitOrder(next: Backlog[]) {
    const previous = order;
    setOrder(next);
    setReorderError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/backlogs/order`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ backlogIds: next.map((b) => b.id) }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setOrder(previous);
        setReorderError(body?.error.message ?? "Failed to reorder backlogs.");
      }
    } catch {
      setOrder(previous);
      setReorderError("Failed to reorder backlogs.");
    }
  }

  function moveBacklog(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= order.length) return;
    void commitOrder(moveItem(order, index, target));
  }

  function handleDrop(index: number) {
    const fromIndex = order.findIndex((b) => b.id === draggingId);
    setDraggingId(null);
    if (fromIndex === -1 || fromIndex === index) return;
    void commitOrder(moveItem(order, fromIndex, index));
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Backlogs</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            {/* The view modes only make sense once backlogs exist, but "New
                backlog" must stay reachable on an empty project. */}
            {backlogs.length > 0 ? (
              <ViewModeToggle value={view} onChange={setView} />
            ) : null}
            {!creating ? (
              <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
                <Plus className="size-4" aria-hidden />
                New backlog
              </Button>
            ) : null}
          </div>
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
        ) : view === "board" ? (
          <BacklogBoardSection projectId={projectId} backlogs={order} />
        ) : view === "timeline" ? (
          <BacklogTimelineSection projectId={projectId} backlogs={backlogs} />
        ) : (
          <div className="space-y-2">
            {reorderError ? (
              <Alert variant="destructive">
                <AlertDescription>{reorderError}</AlertDescription>
              </Alert>
            ) : null}
            <ul className="space-y-2">
              {order.map((backlog, index) => (
              <li
                key={backlog.id}
                className="border-border rounded-md border px-3 py-2"
                onDragOver={(e) => e.preventDefault()}
                onDrop={(e) => {
                  e.preventDefault();
                  handleDrop(index);
                }}
              >
                {editingId === backlog.id ? (
                  <EditBacklogForm
                    backlog={backlog}
                    onSaved={() => setEditingId(null)}
                    onCancel={() => setEditingId(null)}
                  />
                ) : (
                  <div className="flex items-center justify-between gap-4">
                    <div className="flex shrink-0 flex-col items-center self-stretch">
                      <button
                        type="button"
                        aria-label={`Move ${backlog.name} up`}
                        disabled={index === 0}
                        onClick={() => moveBacklog(index, -1)}
                        className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                      >
                        <ChevronUp className="size-4" />
                      </button>
                      <span
                        draggable
                        aria-hidden="true"
                        onDragStart={() => setDraggingId(backlog.id)}
                        onDragEnd={() => setDraggingId(null)}
                        className="text-muted-foreground cursor-grab active:cursor-grabbing"
                      >
                        <GripVertical className="size-4" />
                      </span>
                      <button
                        type="button"
                        aria-label={`Move ${backlog.name} down`}
                        disabled={index === order.length - 1}
                        onClick={() => moveBacklog(index, 1)}
                        className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                      >
                        <ChevronDown className="size-4" />
                      </button>
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Link
                          href={backlogPath(projectId, backlog.id)}
                          className="text-foreground text-sm hover:underline"
                        >
                          {backlog.name}{" "}
                          <span className="text-muted-foreground text-xs">({backlog.taskCount})</span>
                        </Link>
                        <PriorityBadge priority={backlog.priority} />
                        <ProgressBadge progress={backlog.progress} />
                      </div>
                      {backlogScheduleLabel(backlog) ? (
                        <p className="text-muted-foreground truncate text-xs">
                          {backlogScheduleLabel(backlog)}
                        </p>
                      ) : null}
                    </div>
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
                        Edit
                      </Button>
                      <DeleteBacklogButton backlog={backlog} />
                    </div>
                  </div>
                )}
              </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
