"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { backlogPath, tasksPath } from "@/lib/routes";
import { backlogScheduleLabel } from "@/lib/backlogs";
import { isCardBackgroundClick } from "@/lib/cards";
import { backlogCompletion } from "@/lib/timeline";
import { PROGRESS_ACCENT, PROGRESS_COLUMNS } from "@/lib/progress";
import type { ApiError, Backlog, Progress, Task } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { PriorityBadge } from "@/components/PriorityBadge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/**
 * BacklogBoardSection is the Board view mode of the Backlog collection: one
 * column per progress stage, cards stacked top to bottom inside it
 * (docs/ui-design.md rule 5 — the same dataset the List and Timeline modes
 * show).
 *
 * Dragging a card to another column changes that backlog's own progress, which
 * is the action the layout implies; the per-card progress select does the same
 * thing for keyboard and touch users, the same way the List mode pairs its drag
 * handle with move-up/down buttons. Priority rides along as a badge, not as the
 * axis.
 *
 * A backlog's progress is its own, set here by hand — distinct from the
 * closed/total task ratio each card also shows, which is derived from its tasks
 * and stays read-only.
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
  /** The closed-task ratio comes from tasks, so a failed fetch has to be
   *  visible rather than showing every backlog as having no tasks. */
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
  const [dragOverProgress, setDragOverProgress] = useState<Progress | null>(null);

  async function changeProgress(backlog: Backlog, progress: Progress) {
    if (backlog.progress === progress) return;

    const previous = items;
    setItems(items.map((b) => (b.id === backlog.id ? { ...b, progress } : b)));
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
          priority: backlog.priority,
          progress,
        }),
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
    const dragged = items.find((b) => b.id === draggingId);
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
      {tasksError ? (
        <p className="text-destructive text-xs">
          Failed to load tasks — the closed-task ratio is unavailable.
        </p>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {PROGRESS_COLUMNS.map((column) => {
          const cards = items.filter((b) => b.progress === column.progress);
          return (
            <section
              key={column.progress}
              aria-label={`${column.label} backlogs`}
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
                <p className="text-muted-foreground px-1 py-4 text-xs">No backlogs.</p>
              ) : (
                <ul className="space-y-2">
                  {cards.map((backlog) => {
                    const completion = backlogCompletion(tasks, backlog.id);
                    const schedule = backlogScheduleLabel(backlog);
                    return (
                      <li
                        key={backlog.id}
                        draggable
                        onDragStart={() => setDraggingId(backlog.id)}
                        onDragEnd={() => {
                          setDraggingId(null);
                          setDragOverProgress(null);
                        }}
                        // The whole card opens the backlog, not just its name —
                        // the card *is* the backlog here. Its own controls (the
                        // name link, "View tasks", the progress select) keep
                        // their behaviour; the name link is also what keeps this
                        // reachable by keyboard, which a click handler alone
                        // would not be.
                        onClick={(e) => {
                          if (isCardBackgroundClick(e)) {
                            router.push(backlogPath(projectId, backlog.id));
                          }
                        }}
                        className={`bg-card border-border hover:border-ring cursor-grab space-y-2 rounded-md border p-3 shadow-xs transition-colors active:cursor-grabbing ${
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
                                style={{ width: `${Math.round(completion.ratio * 100)}%` }}
                              />
                            </div>
                            <p className="text-muted-foreground text-xs tabular-nums">
                              {completion.total === 0
                                ? "No tasks"
                                : `${completion.closed}/${completion.total} closed`}
                            </p>
                          </div>
                        ) : null}

                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <span className="flex items-center gap-2">
                            <Link
                              href={tasksPath(projectId, { backlogId: backlog.id })}
                              className="text-muted-foreground hover:text-foreground text-xs hover:underline"
                            >
                              View tasks
                            </Link>
                            {/* Priority stays on the card because the board's
                                own axis is progress. */}
                            <PriorityBadge priority={backlog.priority} />
                          </span>
                          <Select
                            value={backlog.progress}
                            onValueChange={(value) => void changeProgress(backlog, value as Progress)}
                          >
                            <SelectTrigger
                              size="sm"
                              aria-label={`Progress of ${backlog.name}`}
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
