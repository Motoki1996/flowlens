"use client";

import { useEffect, useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { ChevronDown, ChevronUp, GripVertical, Plus } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { epicPath, tasksPath } from "@/lib/routes";
import { groupScheduleLabel } from "@/lib/groups";
import { groupTaskCompletion } from "@/lib/timeline";
import { useViewMode } from "@/lib/useViewMode";
import type { ApiError, Backlog, Epic, LinkedGitlabProject, Priority, Progress, Task } from "@/types";
import { PROGRESS_COLUMNS, PROGRESS_LABELS } from "@/lib/progress";
import { PRIORITY_COLUMNS, PRIORITY_LABELS } from "@/lib/priority";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CreateFormRegion } from "@/components/CreateFormRegion";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PriorityBadge } from "@/components/PriorityBadge";
import { ProgressBadge } from "@/components/ProgressBadge";
import { EpicForm } from "@/components/EpicForm";
import { EpicDeleteButton } from "@/components/EpicDeleteButton";
import { EpicBoardSection } from "@/components/EpicBoardSection";
import { TaskSearchBox } from "@/components/TaskSearchBox";
import { ViewModeToggle, type ViewMode } from "@/components/ViewModeToggle";

/** The sort values the Epic collection's `?sort=` accepts, the same set the
 *  Backlog collection's does: "manual" is the API's own drag-reorderable
 *  `position` order, "priority"/"progress" are applied server-side, and
 *  "dueOn" is applied here because the API has no concept of it. */
type EpicSort = "manual" | "dueOn" | "priority" | "progress";

/** The `?backlog=` value standing for "epics in no backlog at all". The API
 *  spells it "unassigned" on `backlog_id`; the URL says "none" because on
 *  this screen it is the epic that is unfiled, not a person. */
export const NO_BACKLOG_FILTER = "none";

function compareByDueOn(a: Epic, b: Epic): number {
  if (!a.dueOn && !b.dueOn) return 0;
  if (!a.dueOn) return 1;
  if (!b.dueOn) return -1;
  return a.dueOn.localeCompare(b.dueOn);
}

/** The Timeline view mode pulls in the charting library the default views
 *  have no use for — loading it on demand keeps that cost off the collection
 *  until someone actually switches views, the same arrangement the Backlog
 *  and Task collections use. */
const EpicTimelineSection = dynamic(
  () => import("@/components/EpicTimelineSection").then((m) => m.EpicTimelineSection),
  {
    loading: () => <p className="text-muted-foreground text-sm">Loading timeline…</p>,
  },
);

function moveItem<T>(list: T[], fromIndex: number, toIndex: number): T[] {
  const next = [...list];
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  return next;
}

/** The one place that defines what "no filter" means for each URL-held
 *  filter, mirroring BacklogListSection's own FILTER_DEFAULTS. */
const FILTER_DEFAULTS = {
  backlog: "all",
  priority: "all",
  progress: "all",
  sort: "manual",
} as const;

/**
 * EpicListSection is the Epic collection view at /projects/[projectId]/epics.
 * Board (the default), List and Timeline are view modes of this one screen
 * (docs/ui-design.md rule 5), and epic creation, editing and delete all
 * happen here rather than on a separate management screen — actions live on
 * the object they act on (rule 4).
 *
 * `backlog`/`priority`/`progress`/`sort` live in the URL and are applied
 * server-side by the caller (page.tsx), the same as the Backlog collection's
 * own filters — except `sort=dueOn`, which has no server-side meaning and is
 * sorted here, and the name search (`?q=`), which has no API support at all
 * and is matched client-side, since epics run far fewer per project than
 * tasks.
 *
 * As with a backlog's own order, `PATCH .../epics/order` requires *every*
 * current epic in one request (epic.Service.Reorder), so any filter, search
 * or non-manual sort hides drag-and-drop and the move buttons rather than
 * sending a request certain to fail with an ID mismatch.
 *
 * Unlike the Backlog collection, there is no trailing "Unclassified" row: an
 * epic outside any backlog is still an epic and appears as its own row, with
 * its missing backlog shown on the row rather than as a separate group.
 */
export function EpicListSection({
  projectId,
  epics,
  backlogs = [],
  tasks = [],
  links = [],
  backlogFilter,
  priorityFilter,
  progressFilter,
  sort = "manual",
  initialView = "board",
  error = false,
}: {
  projectId: string;
  epics: Epic[];
  /** The project's backlogs, offered by the create/edit forms as the epic's
   *  parent and by the filter row as `?backlog=`. */
  backlogs?: Backlog[];
  /** The project's tasks, for the create/edit form's "tasks in this epic"
   *  picker. Empty drops that field. */
  tasks?: Task[];
  /** The project's linked GitLab projects; empty hides that form field. */
  links?: LinkedGitlabProject[];
  /** The applied `?backlog=` — a backlog UUID, NO_BACKLOG_FILTER, or
   *  undefined for all of them. */
  backlogFilter?: string;
  /** The applied `?priority=`; undefined means all of them. */
  priorityFilter?: Priority;
  /** The applied `?progress=`; undefined means all of them. */
  progressFilter?: Progress;
  /** The applied `?sort=`, or "manual" for the API's own position order. */
  sort?: EpicSort;
  /** The applied `?view=`; Board is the default, for the same reason it is on
   *  the Backlog collection — how far along each epic is, is the first
   *  question asked of it. */
  initialView?: ViewMode;
  /** Set when page.tsx's getEpics call failed: the view toggle and filter row
   *  hide, "New epic" stays reachable, and the content area reports the
   *  failure instead of an empty list. */
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [view, setView] = useViewMode(initialView);

  // `order` mirrors `epics` but is reordered optimistically on drag/move,
  // ahead of the PATCH .../epics/order round trip — a router.refresh() per
  // drag doesn't read as drag-and-drop at all. It resyncs whenever the server
  // data changes under it.
  const [order, setOrder] = useState(epics);
  useEffect(() => setOrder(epics), [epics]);
  const [reorderError, setReorderError] = useState<string | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);

  const search = (searchParams.get("q") ?? "").trim();

  const backlogNames = useMemo(
    () => new Map(backlogs.map((b) => [b.id, b.name])),
    [backlogs],
  );

  const canReorder =
    sort === "manual" &&
    !backlogFilter &&
    !priorityFilter &&
    !progressFilter &&
    search === "";

  const visibleEpics = useMemo(() => {
    let result = order;
    if (search) {
      const q = search.toLowerCase();
      result = result.filter((e) => e.name.toLowerCase().includes(q));
    }
    if (sort === "dueOn") {
      result = [...result].sort(compareByDueOn);
    }
    return result;
  }, [order, search, sort]);

  const hasActiveFilters =
    backlogFilter !== undefined ||
    priorityFilter !== undefined ||
    progressFilter !== undefined ||
    sort !== FILTER_DEFAULTS.sort ||
    search !== "";

  function updateQuery(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(next)) {
      params.delete(key);
      if (value) params.set(key, value);
    }
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  function clearFilters() {
    router.push(pathname);
  }

  /** Distinguishes *why* the list came back empty — a bare "no matches"
   *  doesn't say whether it's the search term or one of the filters. */
  function emptyFilterMessage(): string {
    if (search) return `No epics match "${search}".`;
    if (backlogFilter === NO_BACKLOG_FILTER) return "No epics outside a backlog.";
    if (backlogFilter) {
      return `No epics in ${backlogNames.get(backlogFilter) ?? "that backlog"}.`;
    }
    if (priorityFilter) {
      return `No ${PRIORITY_LABELS[priorityFilter].toLowerCase()} priority epics.`;
    }
    if (progressFilter) {
      return `No ${PROGRESS_LABELS[progressFilter].toLowerCase()} epics.`;
    }
    return "No epics match the current filters.";
  }

  async function commitOrder(next: Epic[]) {
    const previous = order;
    setOrder(next);
    setReorderError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/epics/order`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ epicIds: next.map((e) => e.id) }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setOrder(previous);
        setReorderError(body?.error.message ?? "Failed to reorder epics.");
      }
    } catch {
      setOrder(previous);
      setReorderError("Failed to reorder epics.");
    }
  }

  function moveEpic(index: number, direction: -1 | 1) {
    if (!canReorder) return;
    const target = index + direction;
    if (target < 0 || target >= order.length) return;
    void commitOrder(moveItem(order, index, target));
  }

  function handleDrop(index: number) {
    if (!canReorder) return;
    const fromIndex = order.findIndex((e) => e.id === draggingId);
    setDraggingId(null);
    if (fromIndex === -1 || fromIndex === index) return;
    void commitOrder(moveItem(order, fromIndex, index));
  }

  const showControls = !error && (epics.length > 0 || hasActiveFilters);

  return (
    <Card>
      <CardHeader>
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <CardTitle className="text-base font-medium">Epics</CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              {showControls ? <ViewModeToggle value={view} onChange={setView} /> : null}
              {!creating ? (
                <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
                  <Plus className="size-4" aria-hidden />
                  New epic
                </Button>
              ) : null}
            </div>
          </div>
          {/* Filters belong to the collection, not to one presentation of it
              (docs/ui-design.md rule 5), so they stay put across view modes. */}
          {showControls ? (
            <div className="flex flex-wrap items-center gap-2">
              <TaskSearchBox
                value={search}
                onChange={(value) => updateQuery({ q: value.trim() === "" ? undefined : value })}
                label="epics"
              />
              <Select
                value={backlogFilter ?? "all"}
                onValueChange={(value) =>
                  updateQuery({ backlog: value === FILTER_DEFAULTS.backlog ? undefined : value })
                }
              >
                <SelectTrigger size="sm" aria-label="Backlog" className="w-44">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All backlogs</SelectItem>
                  <SelectItem value={NO_BACKLOG_FILTER}>No backlog</SelectItem>
                  {backlogs.map((backlog) => (
                    <SelectItem key={backlog.id} value={backlog.id}>
                      {backlog.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={priorityFilter ?? "all"}
                onValueChange={(value) =>
                  updateQuery({ priority: value === FILTER_DEFAULTS.priority ? undefined : value })
                }
              >
                <SelectTrigger size="sm" aria-label="Priority" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All priorities</SelectItem>
                  {PRIORITY_COLUMNS.map((option) => (
                    <SelectItem key={option.priority} value={option.priority}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={progressFilter ?? "all"}
                onValueChange={(value) =>
                  updateQuery({ progress: value === FILTER_DEFAULTS.progress ? undefined : value })
                }
              >
                <SelectTrigger size="sm" aria-label="Progress" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All progress</SelectItem>
                  {PROGRESS_COLUMNS.map((option) => (
                    <SelectItem key={option.progress} value={option.progress}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={sort}
                onValueChange={(value) =>
                  updateQuery({ sort: value === FILTER_DEFAULTS.sort ? undefined : value })
                }
              >
                <SelectTrigger size="sm" aria-label="Sort" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="manual">Manual order</SelectItem>
                  <SelectItem value="dueOn">Due date</SelectItem>
                  <SelectItem value="priority">Priority</SelectItem>
                  <SelectItem value="progress">Progress</SelectItem>
                </SelectContent>
              </Select>
              {hasActiveFilters ? (
                <button
                  type="button"
                  onClick={clearFilters}
                  className="text-muted-foreground hover:text-foreground text-xs underline"
                >
                  Clear filters
                </button>
              ) : null}
            </div>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {creating ? (
          <CreateFormRegion>
            <EpicForm
              projectId={projectId}
              backlogs={backlogs}
              tasks={tasks}
              links={links}
              defaultBacklogId={
                backlogFilter && backlogFilter !== NO_BACKLOG_FILTER ? backlogFilter : null
              }
              onSaved={() => {
                router.refresh();
                setCreating(false);
              }}
              onCancel={() => setCreating(false)}
            />
          </CreateFormRegion>
        ) : null}

        {error ? (
          <p className="text-destructive text-sm">
            Failed to load epics. Try refreshing the page.
          </p>
        ) : visibleEpics.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            {hasActiveFilters ? emptyFilterMessage() : "No epics yet."}
          </p>
        ) : view === "board" ? (
          <EpicBoardSection projectId={projectId} epics={visibleEpics} />
        ) : view === "timeline" ? (
          <EpicTimelineSection projectId={projectId} epics={visibleEpics} />
        ) : (
          <div className="space-y-2">
            {reorderError ? (
              <Alert variant="destructive">
                <AlertDescription>{reorderError}</AlertDescription>
              </Alert>
            ) : null}
            <ul className="space-y-2">
              {visibleEpics.map((epic, index) => {
                const completion = groupTaskCompletion(epic);
                const schedule = groupScheduleLabel(epic);
                const backlogName = epic.backlogId ? backlogNames.get(epic.backlogId) : undefined;
                return (
                  <li
                    key={epic.id}
                    className="border-border rounded-md border px-3 py-2"
                    onDragOver={(e) => canReorder && e.preventDefault()}
                    onDrop={(e) => {
                      if (!canReorder) return;
                      e.preventDefault();
                      handleDrop(index);
                    }}
                  >
                    {editingId === epic.id ? (
                      <EpicForm
                        projectId={projectId}
                        epic={epic}
                        backlogs={backlogs}
                        tasks={tasks}
                        links={links}
                        onSaved={() => {
                          // The row is rendered from the server-fetched list,
                          // so the saved values only appear after a refresh.
                          router.refresh();
                          setEditingId(null);
                        }}
                        onCancel={() => setEditingId(null)}
                      />
                    ) : (
                      <div className="flex items-center justify-between gap-4">
                        {canReorder ? (
                          <div className="flex shrink-0 flex-col items-center self-stretch">
                            <button
                              type="button"
                              aria-label={`Move ${epic.name} up`}
                              disabled={index === 0}
                              onClick={() => moveEpic(index, -1)}
                              className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                            >
                              <ChevronUp className="size-4" />
                            </button>
                            <span
                              draggable
                              aria-hidden="true"
                              onDragStart={() => setDraggingId(epic.id)}
                              onDragEnd={() => setDraggingId(null)}
                              className="text-muted-foreground cursor-grab active:cursor-grabbing"
                            >
                              <GripVertical className="size-4" />
                            </span>
                            <button
                              type="button"
                              aria-label={`Move ${epic.name} down`}
                              disabled={index === visibleEpics.length - 1}
                              onClick={() => moveEpic(index, 1)}
                              className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                            >
                              <ChevronDown className="size-4" />
                            </button>
                          </div>
                        ) : null}
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <Link
                              href={epicPath(projectId, epic.id)}
                              className="text-foreground text-sm hover:underline"
                            >
                              {epic.name}
                            </Link>
                            <PriorityBadge priority={epic.priority} />
                            <ProgressBadge progress={epic.progress} />
                          </div>
                          <p className="text-muted-foreground truncate text-xs">
                            {backlogName ?? "No backlog"}
                            {schedule ? ` · ${schedule}` : ""}
                            {epic.baseBranch ? ` · ${epic.baseBranch}` : ""}
                          </p>
                          {/* The fill is a second reading of the ratio stated
                              beside it, never the only one — the same rule the
                              Board mode's cards and the timeline's bars
                              follow. */}
                          <div className="mt-1 flex items-center gap-2">
                            <div className="bg-muted h-1 w-24 shrink-0 overflow-hidden rounded-full">
                              <div
                                aria-hidden
                                className="bg-primary h-full"
                                style={{ width: `${Math.round(completion.ratio * 100)}%` }}
                              />
                            </div>
                            <span className="text-muted-foreground text-xs tabular-nums">
                              {completion.total === 0
                                ? "No tasks"
                                : `${completion.closed}/${completion.total} closed`}
                            </span>
                          </div>
                        </div>
                        <div className="flex shrink-0 items-center gap-2">
                          {/* Tasks live in the Task collection, filtered — this
                              row hands off to it rather than growing a second
                              place to browse tasks (docs/ui-design.md rule 5). */}
                          <Link
                            href={tasksPath(projectId, { epicId: epic.id })}
                            className="text-muted-foreground hover:text-foreground text-sm hover:underline"
                          >
                            View tasks
                          </Link>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => setEditingId(epic.id)}
                          >
                            Edit
                          </Button>
                          <EpicDeleteButton epic={epic} onDeleted={() => router.refresh()} />
                        </div>
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
