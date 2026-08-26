"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { isCardBackgroundClick } from "@/lib/cards";
import { groupTaskCompletion } from "@/lib/timeline";
import { GROUP_CONFIG, groupScheduleLabel, type GroupKind, type Grouping } from "@/lib/groups";
import { PROGRESS_ACCENT, PROGRESS_COLUMNS } from "@/lib/progress";
import type { ApiError, Progress } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { PriorityBadge } from "@/components/PriorityBadge";
import { TruncatedName } from "@/components/TruncatedName";

/**
 * GroupBoardSection is the Board view mode shared by the Backlog and Epic
 * collections (lib/groups.ts): one column per progress stage, cards stacked
 * top to bottom inside it (docs/ui-design.md rule 5 — the same dataset the
 * List and Timeline modes show).
 *
 * Dragging a card to another column changes that object's own progress, which
 * is the action the layout implies. Priority rides along as a badge, not as
 * the axis.
 *
 * That progress is the object's own, set here by hand — distinct from the
 * closed/total task ratio each card also shows, which is read off its
 * taskCount/closedTaskCount (issue #144) and stays read-only.
 */
export function GroupBoardSection({
  projectId,
  kind,
  items: backlogs,
}: {
  projectId: string;
  kind: GroupKind;
  items: Grouping[];
}) {
  const config = GROUP_CONFIG[kind];
  const router = useRouter();

  // `items` mirrors `backlogs` but is updated optimistically on a drop, ahead
  // of the PATCH round trip: a router.refresh() per drag doesn't read as
  // drag-and-drop. It resyncs whenever the server data changes under it.
  const [items, setItems] = useState(backlogs);
  useEffect(() => setItems(backlogs), [backlogs]);
  const [error, setError] = useState<string | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [dragOverProgress, setDragOverProgress] = useState<Progress | null>(null);

  async function changeProgress(backlog: Grouping, progress: Progress) {
    if (backlog.progress === progress) return;

    const previous = items;
    setItems(items.map((b) => (b.id === backlog.id ? { ...b, progress } : b)));
    setError(null);
    try {
      // Every other column is left out of the body: a PATCH is a partial
      // update, so an absent key keeps its stored value — which is what stops
      // this from resetting an epic's backlog as a side effect of a drag.
      const res = await fetch(`${API_PUBLIC_URL}${config.apiPath(backlog.id)}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          name: backlog.name,
          description: backlog.description,
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
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {PROGRESS_COLUMNS.map((column) => {
          const cards = items.filter((b) => b.progress === column.progress);
          return (
            <section
              key={column.progress}
              aria-label={`${column.label} ${config.plural}`}
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
                <p className="text-muted-foreground px-1 py-4 text-xs">No {config.plural}.</p>
              ) : (
                <ul className="space-y-2">
                  {cards.map((backlog) => {
                    const completion = groupTaskCompletion(backlog);
                    const schedule = groupScheduleLabel(backlog);
                    return (
                      <li
                        key={backlog.id}
                        draggable
                        onDragStart={() => setDraggingId(backlog.id)}
                        onDragEnd={() => {
                          setDraggingId(null);
                          setDragOverProgress(null);
                        }}
                        // The whole card opens the object, not just its name —
                        // the card *is* the object here. Its own controls (the
                        // name link, "View tasks") keep their behaviour; the
                        // name link is also what keeps this reachable by
                        // keyboard, which a click handler alone would not be.
                        onClick={(e) => {
                          if (isCardBackgroundClick(e)) {
                            router.push(config.detailPath(projectId, backlog.id));
                          }
                        }}
                        className={`bg-card border-border hover:border-ring cursor-grab space-y-2 rounded-md border p-3 shadow-xs transition-colors active:cursor-grabbing ${
                          draggingId === backlog.id ? "opacity-50" : ""
                        }`}
                      >
                        {/* Two lines, then the rest on hover: a card is only
                            as wide as its column, and a name left to run freely
                            stretched the card instead of fitting it. */}
                        <TruncatedName
                          href={config.detailPath(projectId, backlog.id)}
                          text={backlog.name}
                          lines={2}
                          className="text-foreground text-sm font-medium hover:underline"
                        />

                        {schedule ? (
                          <p className="text-muted-foreground truncate text-xs">{schedule}</p>
                        ) : null}

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

                        <div className="flex flex-wrap items-center gap-2">
                          <Link
                            href={config.tasksPath(projectId, backlog.id)}
                            className="text-muted-foreground hover:text-foreground text-xs hover:underline"
                          >
                            View tasks
                          </Link>
                          {/* Priority stays on the card because the board's own
                              axis is progress. */}
                          <PriorityBadge priority={backlog.priority} />
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
