"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { taskPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";
import { isCardBackgroundClick } from "@/lib/cards";
import { PROGRESS_ACCENT, PROGRESS_COLUMNS } from "@/lib/progress";
import type { ApiError, Backlog, Progress, Task } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PriorityBadge } from "@/components/PriorityBadge";
import { SyncBadge } from "@/components/SyncBadge";

const UNCLASSIFIED_LABEL = "Unclassified";

/**
 * TaskBoardSection is the Board view mode of the Task collection: one column
 * per progress stage, a card per task stacked inside it (docs/ui-design.md
 * rule 5 — the same dataset the List and Timeline modes show, and it receives
 * the screen's already filtered and sorted tasks so every mode narrows
 * together).
 *
 * Dragging a card to another column changes that task's progress, which is the
 * action the layout implies; the per-card progress select does the same for
 * keyboard and touch users. Priority rides along as a badge, not as the axis: a
 * task's priority doesn't change by moving the work forward. Reassigning a
 * task's *backlog* stays in the List mode, which groups by backlog — this
 * board's axis is progress only, so the card names its backlog rather than
 * letting it be changed here.
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
  const [dragOverProgress, setDragOverProgress] = useState<Progress | null>(null);

  const backlogNames = new Map(backlogs.map((b) => [b.id, b.name]));

  async function changeProgress(task: Task, progress: Progress) {
    if (task.progress === progress) return;

    const previous = items;
    setItems(items.map((t) => (t.id === task.id ? { ...t, progress } : t)));
    setError(null);
    try {
      // The task PATCH is a partial update, so progress travels alone — see the
      // "Task & backlog progress" section in README.md. It never touches
      // status: closing a task stays with /close, and GitLab never hears about
      // progress at all.
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ progress }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setItems(previous);
        setError(body?.error.message ?? "Failed to change progress.");
        return;
      }
      router.refresh();
    } catch {
      setItems(previous);
      setError("Failed to change progress.");
    }
  }

  function handleDrop(progress: Progress) {
    const dragged = items.find((t) => t.id === draggingId);
    setDraggingId(null);
    setDragOverProgress(null);
    if (!dragged) return;
    void changeProgress(dragged, progress);
  }

  return (
    <div className="space-y-3">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {PROGRESS_COLUMNS.map((column) => {
          const cards = items.filter((t) => t.progress === column.progress);
          return (
            <section
              key={column.progress}
              aria-label={`${column.label} tasks`}
              onDragOver={(e) => {
                e.preventDefault();
                setDragOverProgress(column.progress);
              }}
              onDragLeave={() => setDragOverProgress((p) => (p === column.progress ? null : p))}
              onDrop={(e) => {
                e.preventDefault();
                handleDrop(column.progress);
              }}
              className={`bg-muted/40 rounded-md p-2 ${
                dragOverProgress === column.progress ? "ring-primary/50 ring-2" : ""
              }`}
            >
              <div className="mb-2 flex items-center gap-2 px-1">
                <span
                  aria-hidden
                  className={`size-2 rounded-full ${PROGRESS_ACCENT[column.progress]}`}
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
                        setDragOverProgress(null);
                      }}
                      // The whole card opens the task, not just its title — the
                      // card *is* the task here. Its own controls (the title
                      // link, the progress select) keep their behaviour; the
                      // title link is also what keeps this reachable by
                      // keyboard, which a click handler alone would not be.
                      onClick={(e) => {
                        if (isCardBackgroundClick(e)) router.push(taskPath(projectId, task.id));
                      }}
                      className={`bg-card border-border hover:border-ring cursor-grab space-y-2 rounded-md border p-3 shadow-xs transition-colors active:cursor-grabbing ${
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
                          {/* Status and priority stay on the card because the
                              board's own axis is progress — a closed or urgent
                              task must not read as open or ordinary just
                              because it sits in the In progress column. */}
                          <Badge variant={task.status === "open" ? "default" : "secondary"}>
                            {task.status === "open" ? "Open" : "Closed"}
                          </Badge>
                          <PriorityBadge priority={task.priority} />
                          <SyncBadge gitlab={task.gitlab} />
                        </span>
                        <Select
                          value={task.progress}
                          onValueChange={(value) => void changeProgress(task, value as Progress)}
                        >
                          <SelectTrigger
                            size="sm"
                            aria-label={`Progress of ${task.title}`}
                            className="h-7 w-32 text-xs"
                          >
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
