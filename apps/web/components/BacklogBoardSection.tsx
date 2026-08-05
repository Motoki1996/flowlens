"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { backlogPath, tasksPath } from "@/lib/routes";
import { backlogScheduleLabel } from "@/lib/backlogs";
import { backlogProgress } from "@/lib/timeline";
import { PRIORITY_ACCENT, PRIORITY_COLUMNS } from "@/lib/priority";
import type { ApiError, Backlog, Priority, Task } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/**
 * BacklogBoardSection is the Board view mode of the Backlog collection: one
 * column per priority, cards stacked top to bottom inside it (docs/ui-design.md
 * rule 5 — the same dataset the List and Timeline modes show).
 *
 * Dragging a card to another column changes that backlog's priority, which is
 * the action the layout implies; the per-card priority select does the same
 * thing for keyboard and touch users, the same way the List mode pairs its drag
 * handle with move-up/down buttons.
 */
export function BacklogBoardSection({
  projectId,
  backlogs,
  tasks = [],
  tasksError = false,
}: {
  projectId: string;
  backlogs: Backlog[];
  tasks?: Task[];
  /** Card progress comes from tasks, so a failed fetch has to be visible rather
   *  than showing every backlog as having no tasks. */
  tasksError?: boolean;
}) {
  const router = useRouter();

  // `items` mirrors `backlogs` but is updated optimistically on a drop, ahead
  // of the PATCH round trip — the same arrangement the List mode's reordering
  // uses, for the same reason: a router.refresh() per drag doesn't read as
  // drag-and-drop. It resyncs whenever the server data changes under it.
  const [items, setItems] = useState(backlogs);
  useEffect(() => setItems(backlogs), [backlogs]);
  const [error, setError] = useState<string | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [dragOverPriority, setDragOverPriority] = useState<Priority | null>(null);

  async function changePriority(backlog: Backlog, priority: Priority) {
    if (backlog.priority === priority) return;

    const previous = items;
    setItems(items.map((b) => (b.id === backlog.id ? { ...b, priority } : b)));
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/backlogs/${backlog.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: backlog.name,
          description: backlog.description,
          position: backlog.position,
          startDate: backlog.startDate,
          dueOn: backlog.dueOn,
          priority,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setItems(previous);
        setError(body?.error.message ?? "Failed to change priority.");
        return;
      }
      router.refresh();
    } catch {
      setItems(previous);
      setError("Failed to change priority.");
    }
  }

  function handleDrop(priority: Priority) {
    const dragged = items.find((b) => b.id === draggingId);
    setDraggingId(null);
    setDragOverPriority(null);
    if (!dragged) return;
    void changePriority(dragged, priority);
  }

  return (
    <div className="space-y-3">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      {tasksError ? (
        <p className="text-destructive text-xs">Failed to load tasks — progress is unavailable.</p>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {PRIORITY_COLUMNS.map((column) => {
          const cards = items.filter((b) => b.priority === column.priority);
          return (
            <section
              key={column.priority}
              aria-label={`${column.label} backlogs`}
              onDragOver={(e) => {
                e.preventDefault();
                setDragOverPriority(column.priority);
              }}
              onDragLeave={() => setDragOverPriority((p) => (p === column.priority ? null : p))}
              onDrop={(e) => {
                e.preventDefault();
                handleDrop(column.priority);
              }}
              className={`bg-muted/40 rounded-md p-2 ${
                dragOverPriority === column.priority ? "ring-primary/50 ring-2" : ""
              }`}
            >
              <div className="mb-2 flex items-center gap-2 px-1">
                <span aria-hidden className={`size-2 rounded-full ${PRIORITY_ACCENT[column.priority]}`} />
                <h3 className="text-foreground text-sm font-medium">{column.label}</h3>
                <span className="text-muted-foreground text-xs tabular-nums">{cards.length}</span>
              </div>

              {cards.length === 0 ? (
                <p className="text-muted-foreground px-1 py-4 text-xs">No backlogs.</p>
              ) : (
                <ul className="space-y-2">
                  {cards.map((backlog) => {
                    const progress = backlogProgress(tasks, backlog.id);
                    const schedule = backlogScheduleLabel(backlog);
                    return (
                      <li
                        key={backlog.id}
                        draggable
                        onDragStart={() => setDraggingId(backlog.id)}
                        onDragEnd={() => {
                          setDraggingId(null);
                          setDragOverPriority(null);
                        }}
                        className={`bg-card border-border cursor-grab space-y-2 rounded-md border p-3 shadow-xs active:cursor-grabbing ${
                          draggingId === backlog.id ? "opacity-50" : ""
                        }`}
                      >
                        <Link
                          href={backlogPath(projectId, backlog.id)}
                          className="text-foreground block text-sm font-medium hover:underline"
                        >
                          {backlog.name}
                        </Link>

                        {schedule ? (
                          <p className="text-muted-foreground truncate text-xs">{schedule}</p>
                        ) : null}

                        {!tasksError ? (
                          <div className="space-y-1">
                            {/* The fill is a second reading of the ratio stated
                                beside it, never the only one — same rule the
                                timeline's bars follow. */}
                            <div className="bg-muted h-1 w-full overflow-hidden rounded-full">
                              <div
                                aria-hidden
                                className="bg-primary h-full"
                                style={{ width: `${Math.round(progress.ratio * 100)}%` }}
                              />
                            </div>
                            <p className="text-muted-foreground text-xs tabular-nums">
                              {progress.total === 0
                                ? "No tasks"
                                : `${progress.closed}/${progress.total} closed`}
                            </p>
                          </div>
                        ) : null}

                        <div className="flex items-center justify-between gap-2">
                          <Link
                            href={tasksPath(projectId, { backlogId: backlog.id })}
                            className="text-muted-foreground hover:text-foreground text-xs hover:underline"
                          >
                            View tasks
                          </Link>
                          <Select
                            value={backlog.priority}
                            onValueChange={(value) => void changePriority(backlog, value as Priority)}
                          >
                            <SelectTrigger
                              size="sm"
                              aria-label={`Priority of ${backlog.name}`}
                              className="h-7 w-28 text-xs"
                            >
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
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}
