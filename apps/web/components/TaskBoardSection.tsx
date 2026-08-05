"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { taskPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import { PRIORITY_ACCENT, PRIORITY_COLUMNS } from "@/lib/priority";
import type { ApiError, Backlog, Priority, Task } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SyncBadge } from "@/components/SyncBadge";

const UNCLASSIFIED_LABEL = "Unclassified";

/**
 * TaskBoardSection is the Board view mode of the Task collection: one column
 * per priority, a card per task stacked inside it (docs/ui-design.md rule 5 —
 * the same dataset the List and Timeline modes show, and it receives the
 * screen's already filtered and sorted tasks so every mode narrows together).
 *
 * Dragging a card to another column changes that task's priority, which is the
 * action the layout implies; the per-card priority select does the same for
 * keyboard and touch users. Reassigning a task's *backlog* stays in the List
 * mode, which groups by backlog — this board's axis is priority only, so the
 * card names its backlog rather than letting it be changed here.
 */
export function TaskBoardSection({
  projectId,
  tasks,
  backlogs = [],
}: {
  projectId: string;
  tasks: Task[];
  /** Names the backlog on each card; the board never changes it. */
  backlogs?: Backlog[];
}) {
  const router = useRouter();

  // `items` mirrors `tasks` but is updated optimistically on a drop, ahead of
  // the PATCH round trip — the same arrangement the List mode's reordering
  // uses, for the same reason: a router.refresh() per drag doesn't read as
  // drag-and-drop. It resyncs whenever the server data changes under it.
  const [items, setItems] = useState(tasks);
  useEffect(() => setItems(tasks), [tasks]);
  const [error, setError] = useState<string | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [dragOverPriority, setDragOverPriority] = useState<Priority | null>(null);

  const backlogNames = new Map(backlogs.map((b) => [b.id, b.name]));

  async function changePriority(task: Task, priority: Priority) {
    if (task.priority === priority) return;

    const previous = items;
    setItems(items.map((t) => (t.id === task.id ? { ...t, priority } : t)));
    setError(null);
    try {
      // The task PATCH is a partial update, so priority travels alone — see the
      // "Task & backlog priority" section in README.md.
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ priority }),
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
    const dragged = items.find((t) => t.id === draggingId);
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

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {PRIORITY_COLUMNS.map((column) => {
          const cards = items.filter((t) => t.priority === column.priority);
          return (
            <section
              key={column.priority}
              aria-label={`${column.label} tasks`}
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
                <span
                  aria-hidden
                  className={`size-2 rounded-full ${PRIORITY_ACCENT[column.priority]}`}
                />
                <h3 className="text-foreground text-sm font-medium">{column.label}</h3>
                <span className="text-muted-foreground text-xs tabular-nums">{cards.length}</span>
              </div>

              {cards.length === 0 ? (
                <p className="text-muted-foreground px-1 py-4 text-xs">No tasks.</p>
              ) : (
                <ul className="space-y-2">
                  {cards.map((task) => (
                    <li
                      key={task.id}
                      draggable
                      onDragStart={() => setDraggingId(task.id)}
                      onDragEnd={() => {
                        setDraggingId(null);
                        setDragOverPriority(null);
                      }}
                      className={`bg-card border-border cursor-grab space-y-2 rounded-md border p-3 shadow-xs active:cursor-grabbing ${
                        draggingId === task.id ? "opacity-50" : ""
                      } ${task.status === "closed" ? "opacity-70" : ""}`}
                    >
                      <Link
                        href={taskPath(projectId, task.id)}
                        className="text-foreground block text-sm hover:underline"
                      >
                        {task.title}
                      </Link>

                      <p className="text-muted-foreground truncate text-xs">
                        {task.backlogId
                          ? (backlogNames.get(task.backlogId) ?? UNCLASSIFIED_LABEL)
                          : UNCLASSIFIED_LABEL}
                        {task.dueOn ? ` · Due ${formatDate(task.dueOn)}` : ""}
                        {task.assigneeGitlabUsername ? ` · ${task.assigneeGitlabUsername}` : ""}
                      </p>

                      {task.labels.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {task.labels.map((label) => (
                            <Badge key={label} variant="outline">
                              {label}
                            </Badge>
                          ))}
                        </div>
                      ) : null}

                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="flex items-center gap-2">
                          {/* Status stays on the card because the board's own
                              axis is priority — a closed task must not read as
                              open just because it sits in the Urgent column. */}
                          <Badge variant={task.status === "open" ? "default" : "secondary"}>
                            {task.status === "open" ? "Open" : "Closed"}
                          </Badge>
                          <SyncBadge gitlab={task.gitlab} />
                        </span>
                        <Select
                          value={task.priority}
                          onValueChange={(value) => void changePriority(task, value as Priority)}
                        >
                          <SelectTrigger
                            size="sm"
                            aria-label={`Priority of ${task.title}`}
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
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}
