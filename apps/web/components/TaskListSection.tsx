"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { ChevronDown, ChevronUp, GripVertical, Plus } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { taskPath, UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { formatDate, toApiDate } from "@/lib/dates";
import type {
  ApiError,
  Backlog,
  Priority,
  Progress,
  Task,
  TaskDependency,
  TaskStatus,
} from "@/types";
import { PROGRESS_COLUMNS, PROGRESS_LABELS } from "@/lib/progress";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
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
import { SyncBadge } from "@/components/SyncBadge";
import { TaskBoardSection } from "@/components/TaskBoardSection";
import { ViewModeToggle, type ViewMode } from "@/components/ViewModeToggle";

/**
 * The Timeline view mode pulls in the charting library, which the default List
 * mode has no use for — loading it on demand keeps that cost off the task
 * collection until someone actually switches views.
 */
const TaskTimelineSection = dynamic(
  () => import("@/components/TaskTimelineSection").then((m) => m.TaskTimelineSection),
  { loading: () => <p className="text-muted-foreground text-sm">Loading timeline…</p> },
);

// "manual" keeps the API's own order (the drag-reorderable `position` field);
// the rest mirror the sort values the cross-project Task collection accepts
// (issue #76's `?sort=dueOn|priority|updatedAt` on `GET /api/v1/tasks`, see
// AllTasksSection) so the two screens don't disagree on what "sort by
// priority" means.
type TaskSort = "manual" | "dueOn" | "priority" | "progress" | "updatedAt";

const UNCLASSIFIED = UNCLASSIFIED_BACKLOG;
const UNCLASSIFIED_LABEL = "Unclassified";

const PRIORITY_RANK: Record<Priority, number> = { urgent: 4, high: 3, medium: 2, low: 1 };

// Progress ranks the other way from priority — not_started first through done,
// matching `?sort=progress` on the API and the Board view's left-to-right axis,
// so the work reads as advancing.
const PROGRESS_RANK: Record<Progress, number> = {
  not_started: 1,
  in_progress: 2,
  on_hold: 3,
  done: 4,
};

/** isProgress narrows a raw `?progress=` value; anything else falls back to
 *  "all" rather than erroring, the same way an unknown `?sort=` does. */
function isProgress(value: string | undefined): value is Progress {
  return value !== undefined && value in PROGRESS_RANK;
}

// dueOn/updatedAt are RFC3339 strings, so a plain string compare already
// sorts chronologically. A missing dueOn always sorts last, matching the
// cross-project collection's `?sort=dueOn` default.
function compareByDueOn(a: Task, b: Task): number {
  if (!a.dueOn && !b.dueOn) return 0;
  if (!a.dueOn) return 1;
  if (!b.dueOn) return -1;
  return a.dueOn.localeCompare(b.dueOn);
}

/** moveItem returns a copy of list with the item at fromIndex relocated to
 *  toIndex, used by both drag-and-drop and the up/down move buttons so the
 *  two interactions produce identical orderings. */
function moveItem<T>(list: T[], fromIndex: number, toIndex: number): T[] {
  const next = [...list];
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  return next;
}

/** applyBucketOrder resequences the tasks named in newOrderIds to that exact
 *  order, leaving every other task's slot in current untouched — used for a
 *  same-backlog reorder, where newOrderIds is that backlog's (or
 *  Unclassified's) full task ID list in its new order. */
function applyBucketOrder(current: Task[], newOrderIds: string[]): Task[] {
  const idSet = new Set(newOrderIds);
  const byId = new Map(current.filter((t) => idSet.has(t.id)).map((t) => [t.id, t]));
  let cursor = 0;
  return current.map((t) => (idSet.has(t.id) ? (byId.get(newOrderIds[cursor++]) ?? t) : t));
}

/** moveTaskToBucket relocates one task to targetBacklogId, landing at
 *  targetIndex among that bucket's other tasks (their relative order is
 *  otherwise unchanged) — used for a drag-and-drop move between backlogs.
 *  Only the moved task's backlogId is updated locally; the caller is
 *  responsible for the matching assign-backlog API call. */
function moveTaskToBucket(
  current: Task[],
  taskId: string,
  targetBacklogId: string | null,
  targetIndex: number,
): Task[] {
  const moved = current.find((t) => t.id === taskId);
  if (!moved) return current;
  const updatedMoved: Task = { ...moved, backlogId: targetBacklogId };
  const withoutMoved = current.filter((t) => t.id !== taskId);
  const targetKey = targetBacklogId ?? UNCLASSIFIED;
  const bucketItems = withoutMoved.filter((t) => (t.backlogId ?? UNCLASSIFIED) === targetKey);
  const anchor = bucketItems[targetIndex];
  if (!anchor) return [...withoutMoved, updatedMoved];
  const anchorIndex = withoutMoved.findIndex((t) => t.id === anchor.id);
  return [...withoutMoved.slice(0, anchorIndex), updatedMoved, ...withoutMoved.slice(anchorIndex)];
}

function StatusBadge({ status }: { status: TaskStatus }) {
  return (
    <Badge variant={status === "open" ? "default" : "secondary"}>
      {status === "open" ? "Open" : "Closed"}
    </Badge>
  );
}

/**
 * NewTaskForm is the inline creation form shown in the task list. Assignee and
 * labels are deliberately absent: on a project with a linked GitLab project the
 * API fills the assignee in itself, and both fields are edited on the task
 * single view instead (issue #80), once the task exists.
 */
function NewTaskForm({
  projectId,
  backlogs,
  onCancel,
}: {
  projectId: string;
  backlogs: Backlog[];
  onCancel: () => void;
}) {
  const router = useRouter();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [backlogId, setBacklogId] = useState(UNCLASSIFIED);
  const [startDate, setStartDate] = useState<Date | undefined>(undefined);
  const [dueOn, setDueOn] = useState<Date | undefined>(undefined);
  const [priority, setPriority] = useState<Priority>("medium");
  const [progress, setProgress] = useState<Progress>("not_started");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  // Unclassified leads the list so a task can always be filed later, and so the
  // control has a selected label even on a project with no backlogs yet.
  const backlogOptions = useMemo(
    () => [
      { value: UNCLASSIFIED, label: UNCLASSIFIED_LABEL },
      ...backlogs.map((b) => ({ value: b.id, label: b.name })),
    ],
    [backlogs],
  );

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!title.trim()) {
      setError("Task title is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/tasks`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title,
          description,
          backlogId: backlogId === UNCLASSIFIED ? null : backlogId,
          startDate: toApiDate(startDate),
          dueOn: toApiDate(dueOn),
          priority,
          progress,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to create task.");
        return;
      }
      router.refresh();
      onCancel();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="New task">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="new-task-title" className="text-foreground block text-sm font-medium">
          Title
        </label>
        <Input
          id="new-task-title"
          name="title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label htmlFor="new-task-description" className="text-foreground block text-sm font-medium">
          Description
        </label>
        <Textarea
          id="new-task-description"
          name="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div>
          <label htmlFor="new-task-backlog" className="text-foreground block text-sm font-medium">
            Backlog
          </label>
          <Combobox
            id="new-task-backlog"
            options={backlogOptions}
            value={backlogId}
            onChange={setBacklogId}
            searchPlaceholder="Search backlogs…"
            emptyText="No backlog found."
            className="mt-1"
          />
        </div>
        <DateField
          id="new-task-start-date"
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField id="new-task-due-on" label="Due date" value={dueOn} onChange={setDueOn} />
      </div>
      <div>
        <label htmlFor="new-task-priority" className="text-foreground block text-sm font-medium">
          Priority
        </label>
        <Select value={priority} onValueChange={(value) => setPriority(value as Priority)}>
          <SelectTrigger id="new-task-priority" className="mt-1 w-full sm:w-40">
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
        <label htmlFor="new-task-progress" className="text-foreground block text-sm font-medium">
          Progress
        </label>
        <Select value={progress} onValueChange={(value) => setProgress(value as Progress)}>
          <SelectTrigger id="new-task-progress" className="mt-1 w-full sm:w-40">
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
          {pending ? "Creating…" : "Create task"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/**
 * TaskListSection is the List view mode of the Task collection at
 * /projects/[projectId]/tasks (no standalone "unclassified" screen). Tasks are
 * grouped by backlog, with a trailing Unclassified group for tasks that have no
 * backlog. Filters narrow which tasks appear within those groups.
 */
export function TaskListSection({
  projectId,
  tasks,
  backlogs,
  dependencies = [],
  initialBacklogFilter,
  initialSearch,
  initialStatusFilter,
  initialSort,
  initialProgressFilter,
  error = false,
}: {
  projectId: string;
  tasks: Task[];
  backlogs: Backlog[];
  dependencies?: TaskDependency[];
  /** The `?backlog=` the screen was opened with, if any — how the backlog
   *  screens hand off to this collection. */
  initialBacklogFilter?: string;
  /** The `?q=` the screen was opened with, if any. */
  initialSearch?: string;
  /** The `?status=` the screen was opened with. Defaults to "open" so closed
   *  tasks don't fill the list — anything other than "all"/"closed" falls
   *  back to that default rather than erroring. */
  initialStatusFilter?: string;
  /** The `?sort=` the screen was opened with. Falls back to "manual" (the
   *  API's own position order) for anything not one of the known values. */
  initialSort?: string;
  /** The `?progress=` the screen was opened with. Defaults to "all": unlike
   *  status, no progress stage is noise worth hiding by default. */
  initialProgressFilter?: string;
  error?: boolean;
}) {
  const router = useRouter();
  const [view, setView] = useState<ViewMode>("list");
  const [creating, setCreating] = useState(false);
  const [statusFilter, setStatusFilter] = useState<"all" | TaskStatus>(
    initialStatusFilter === "all" || initialStatusFilter === "closed" ? initialStatusFilter : "open",
  );
  const [backlogFilter, setBacklogFilter] = useState<"all" | string>(
    initialBacklogFilter ?? "all",
  );
  const [search, setSearch] = useState(initialSearch ?? "");
  const [sort, setSort] = useState<TaskSort>(
    initialSort === "dueOn" ||
      initialSort === "priority" ||
      initialSort === "progress" ||
      initialSort === "updatedAt"
      ? initialSort
      : "manual",
  );
  const [progressFilter, setProgressFilter] = useState<"all" | Progress>(
    isProgress(initialProgressFilter) ? initialProgressFilter : "all",
  );
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [targetBacklogId, setTargetBacklogId] = useState("");
  const [assigning, setAssigning] = useState(false);
  const [assignError, setAssignError] = useState<string | null>(null);

  // `localTasks` mirrors `tasks` but is reordered/reassigned optimistically
  // by drag-and-drop and the up/down move buttons, ahead of the PATCH
  // .../tasks/order round trip — the `fetch` → router.refresh() pattern used
  // elsewhere in this file would otherwise force a full server-component
  // re-render per drag, which doesn't read as drag-and-drop at all (issue
  // #79). It resyncs whenever the server data changes under it.
  const [localTasks, setLocalTasks] = useState(tasks);
  useEffect(() => setLocalTasks(tasks), [tasks]);
  const [reorderError, setReorderError] = useState<string | null>(null);
  const [draggingTaskId, setDraggingTaskId] = useState<string | null>(null);

  // The filter offers every backlog plus the two groupings that aren't
  // backlogs: "all" and the trailing Unclassified group.
  const filterOptions = useMemo(
    () => [
      { value: "all", label: "All backlogs" },
      ...backlogs.map((b) => ({ value: b.id, label: b.name })),
      { value: UNCLASSIFIED, label: UNCLASSIFIED_LABEL },
    ],
    [backlogs],
  );

  // Bulk assign can move a task into any backlog, or back to Unclassified —
  // both are valid destinations, unlike the backlog filter's "all" option.
  const assignOptions = useMemo(
    () => [
      ...backlogs.map((b) => ({ value: b.id, label: b.name })),
      { value: UNCLASSIFIED, label: UNCLASSIFIED_LABEL },
    ],
    [backlogs],
  );

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return localTasks.filter((t) => {
      if (statusFilter !== "all" && t.status !== statusFilter) return false;
      if (progressFilter !== "all" && t.progress !== progressFilter) return false;
      if (backlogFilter !== "all") {
        const key = t.backlogId ?? UNCLASSIFIED;
        if (key !== backlogFilter) return false;
      }
      if (query) {
        const haystack = `${t.title}\n${t.description}`.toLowerCase();
        if (!haystack.includes(query)) return false;
      }
      return true;
    });
  }, [localTasks, statusFilter, progressFilter, backlogFilter, search]);

  // "manual" is the API's own order (filtered inherits it from `tasks`), so
  // there's nothing to re-sort. The rest re-sort a copy — sorting is a
  // display order for this screen only, same as the API's own `?sort=`
  // never rewrites `position` (see the "Task & backlog priority" section in
  // README.md).
  const sorted = useMemo(() => {
    if (sort === "manual") return filtered;
    const list = [...filtered];
    if (sort === "dueOn") {
      list.sort(compareByDueOn);
    } else if (sort === "priority") {
      list.sort((a, b) => PRIORITY_RANK[b.priority] - PRIORITY_RANK[a.priority]);
    } else if (sort === "progress") {
      list.sort((a, b) => PROGRESS_RANK[a.progress] - PROGRESS_RANK[b.progress]);
    } else {
      list.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    }
    return list;
  }, [filtered, sort]);

  const groups = useMemo(() => {
    const byBacklog = new Map<string, Task[]>();
    for (const t of sorted) {
      const key = t.backlogId ?? UNCLASSIFIED;
      const list = byBacklog.get(key) ?? [];
      list.push(t);
      byBacklog.set(key, list);
    }
    const ordered: { key: string; name: string; tasks: Task[] }[] = [];
    for (const backlog of backlogs) {
      const list = byBacklog.get(backlog.id);
      if (list) ordered.push({ key: backlog.id, name: backlog.name, tasks: list });
    }
    const unclassified = byBacklog.get(UNCLASSIFIED);
    if (unclassified) {
      ordered.push({ key: UNCLASSIFIED, name: UNCLASSIFIED_LABEL, tasks: unclassified });
    }
    return ordered;
  }, [sorted, backlogs]);

  /**
   * Every filter/sort choice belongs in the URL: the screen stays shareable
   * and the browser's back button walks the filter history.replaceState
   * keeps that a client-side edit — router.replace would re-render the whole
   * tree just to change a filter the client already applied.
   */
  function updateQueryParam(key: string, value: string | undefined) {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (!value) {
      url.searchParams.delete(key);
    } else {
      url.searchParams.set(key, value);
    }
    window.history.replaceState(null, "", url);
  }

  function changeBacklogFilter(value: string) {
    setBacklogFilter(value);
    updateQueryParam("backlog", value === "all" ? undefined : value);
  }

  function changeStatusFilter(value: "all" | TaskStatus) {
    setStatusFilter(value);
    updateQueryParam("status", value === "open" ? undefined : value);
  }

  function changeProgressFilter(value: "all" | Progress) {
    setProgressFilter(value);
    updateQueryParam("progress", value === "all" ? undefined : value);
  }

  function changeSearch(value: string) {
    setSearch(value);
    updateQueryParam("q", value.trim() === "" ? undefined : value);
  }

  function changeSort(value: TaskSort) {
    setSort(value);
    updateQueryParam("sort", value === "manual" ? undefined : value);
  }

  // Distinguishes *why* the filtered list is empty — a bare "no matches"
  // reads the same whether it's the search term, the status default hiding
  // every closed task, or the backlog picker, so each gets its own wording.
  function emptyFilterMessage(): string {
    const query = search.trim();
    if (query) {
      return `No tasks match "${query}".`;
    }
    if (backlogFilter !== "all") {
      const label = filterOptions.find((o) => o.value === backlogFilter)?.label ?? "this backlog";
      const statusPart = statusFilter === "all" ? "" : `${statusFilter} `;
      return `No ${statusPart}tasks in ${label}.`;
    }
    if (statusFilter !== "all") {
      return `No ${statusFilter} tasks.`;
    }
    if (progressFilter !== "all") {
      return `No ${PROGRESS_LABELS[progressFilter].toLowerCase()} tasks.`;
    }
    return "No tasks match the current filters.";
  }

  function toggleSelected(taskId: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(taskId)) {
        next.delete(taskId);
      } else {
        next.add(taskId);
      }
      return next;
    });
  }

  async function handleAssignSelected() {
    if (!targetBacklogId || selected.size === 0) return;

    setAssigning(true);
    setAssignError(null);
    try {
      const backlogId = targetBacklogId === UNCLASSIFIED ? null : targetBacklogId;
      const responses = await Promise.all(
        Array.from(selected).map((taskId) =>
          fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/assign-backlog`, {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ backlogId }),
          }),
        ),
      );
      if (responses.some((res) => !res.ok)) {
        const failed = responses.find((res) => !res.ok);
        const body = (await failed?.json().catch(() => null)) as ApiError | null;
        setAssignError(body?.error.message ?? "Failed to assign some tasks.");
        return;
      }
      setSelected(new Set());
      setTargetBacklogId("");
      router.refresh();
    } finally {
      setAssigning(false);
    }
  }

  // groupKeyOf mirrors the `groups` memo's own grouping key, so the reorder
  // helpers below and the render's group loop always agree on which bucket a
  // task belongs to.
  function groupKeyOf(t: Task): string {
    return t.backlogId ?? UNCLASSIFIED;
  }

  async function commitSameBucketOrder(groupKey: string, newOrderIds: string[]) {
    const previous = localTasks;
    setLocalTasks((current) => applyBucketOrder(current, newOrderIds));
    setReorderError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/tasks/order`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          backlogId: groupKey === UNCLASSIFIED ? null : groupKey,
          taskIds: newOrderIds,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setLocalTasks(previous);
        setReorderError(body?.error.message ?? "Failed to reorder tasks.");
      }
    } catch {
      setLocalTasks(previous);
      setReorderError("Failed to reorder tasks.");
    }
  }

  async function commitCrossBucketMove(taskId: string, targetGroupKey: string, targetIndex: number) {
    const previous = localTasks;
    const targetBacklogId = targetGroupKey === UNCLASSIFIED ? null : targetGroupKey;
    const next = moveTaskToBucket(localTasks, taskId, targetBacklogId, targetIndex);
    setLocalTasks(next);
    setReorderError(null);
    try {
      const assignRes = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/assign-backlog`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ backlogId: targetBacklogId }),
      });
      if (!assignRes.ok) {
        const body = (await assignRes.json().catch(() => null)) as ApiError | null;
        setLocalTasks(previous);
        setReorderError(body?.error.message ?? "Failed to move task.");
        return;
      }
      const orderedIds = next.filter((t) => groupKeyOf(t) === targetGroupKey).map((t) => t.id);
      const orderRes = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/tasks/order`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ backlogId: targetBacklogId, taskIds: orderedIds }),
      });
      if (!orderRes.ok) {
        const body = (await orderRes.json().catch(() => null)) as ApiError | null;
        // The backlog move itself already succeeded server-side here; only
        // the position within the new backlog failed to apply. Reverting the
        // whole local move keeps the UI's error state simple, at the cost of
        // briefly disagreeing with the server until the next refresh.
        setLocalTasks(previous);
        setReorderError(body?.error.message ?? "Failed to reorder tasks.");
      }
    } catch {
      setLocalTasks(previous);
      setReorderError("Failed to move task.");
    }
  }

  function moveTaskWithinGroup(groupKey: string, groupTasks: Task[], index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= groupTasks.length) return;
    void commitSameBucketOrder(
      groupKey,
      moveItem(
        groupTasks.map((t) => t.id),
        index,
        target,
      ),
    );
  }

  function handleDropOnTask(groupKey: string, groupTasks: Task[], targetIndex: number) {
    const taskId = draggingTaskId;
    setDraggingTaskId(null);
    if (!taskId) return;
    const dragged = localTasks.find((t) => t.id === taskId);
    if (!dragged) return;
    if (groupKeyOf(dragged) === groupKey) {
      const fromIndex = groupTasks.findIndex((t) => t.id === taskId);
      if (fromIndex === -1 || fromIndex === targetIndex) return;
      void commitSameBucketOrder(
        groupKey,
        moveItem(
          groupTasks.map((t) => t.id),
          fromIndex,
          targetIndex,
        ),
      );
    } else {
      void commitCrossBucketMove(taskId, groupKey, targetIndex);
    }
  }

  return (
    <Card>
      <CardHeader>
        {/* Two rows, same shape as the Backlog collection: the object's name and
            its object-level controls (view mode, create) on the top row, and the
            filter/sort controls left-aligned on their own row below — crowding
            all of them into one right-aligned cluster made the view toggle and
            "New task" hard to find among the filters. */}
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <CardTitle className="text-base font-medium">Tasks</CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              {/* The view modes only make sense once tasks exist, but "New task"
                  must stay reachable on an empty project. */}
              {!error && tasks.length > 0 ? (
                <ViewModeToggle value={view} onChange={setView} />
              ) : null}
              {!creating ? (
                <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
                  <Plus className="size-4" aria-hidden />
                  New task
                </Button>
              ) : null}
            </div>
          </div>
          {/* Filters belong to the collection, not to one presentation of it
              (docs/ui-design.md rule 5), so they stay put across view modes and
              narrow the timeline the same way they narrow the list. Keeping them
              mounted is also what holds the row above still: unmounting them on
              every view switch slid the buttons out from under the pointer that
              had just clicked them.

              Status is a short fixed list, so it stays a Select; backlogs grow
              with the project and get the searchable Combobox. */}
          {!error && tasks.length > 0 ? (
            <div className="flex flex-wrap items-center gap-2">
              <Input
                aria-label="Search tasks"
                placeholder="Search tasks…"
                value={search}
                onChange={(e) => changeSearch(e.target.value)}
                className="h-8 w-40"
              />
              <Select
                value={statusFilter}
                onValueChange={(value) => changeStatusFilter(value as "all" | TaskStatus)}
              >
                <SelectTrigger size="sm" aria-label="Status" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All statuses</SelectItem>
                  <SelectItem value="open">Open</SelectItem>
                  <SelectItem value="closed">Closed</SelectItem>
                </SelectContent>
              </Select>
              <Select
                value={progressFilter}
                onValueChange={(value) => changeProgressFilter(value as "all" | Progress)}
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
              <Combobox
                aria-label="Backlog"
                options={filterOptions}
                value={backlogFilter}
                onChange={changeBacklogFilter}
                size="sm"
                className="w-44"
                searchPlaceholder="Search backlogs…"
                emptyText="No backlog found."
              />
              <Select value={sort} onValueChange={(value) => changeSort(value as TaskSort)}>
                <SelectTrigger size="sm" aria-label="Sort" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="manual">Manual order</SelectItem>
                  <SelectItem value="dueOn">Due date</SelectItem>
                  <SelectItem value="priority">Priority</SelectItem>
                  <SelectItem value="progress">Progress</SelectItem>
                  <SelectItem value="updatedAt">Recently updated</SelectItem>
                </SelectContent>
              </Select>
            </div>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {creating ? (
          <div className="mb-4">
            <NewTaskForm
              projectId={projectId}
              backlogs={backlogs}
              onCancel={() => setCreating(false)}
            />
          </div>
        ) : null}
        {error ? (
          <p className="text-destructive text-sm">Failed to load tasks. Try refreshing the page.</p>
        ) : tasks.length === 0 ? (
          <p className="text-muted-foreground text-sm">No tasks yet.</p>
        ) : filtered.length === 0 ? (
          // Checked before the view branch so the timeline doesn't answer an
          // empty filter result with its own "set a start or due date" hint.
          <p className="text-muted-foreground text-sm">{emptyFilterMessage()}</p>
        ) : view === "board" ? (
          <TaskBoardSection projectId={projectId} tasks={sorted} backlogs={backlogs} />
        ) : view === "timeline" ? (
          <TaskTimelineSection
            projectId={projectId}
            tasks={sorted}
            allTasks={tasks}
            dependencies={dependencies}
          />
        ) : (
          <div className="space-y-4">
            {/* Manual reordering only makes sense against the API's own
                position order — while sorted by due date/priority/recency,
                a drag would silently fight the display order it's shown in
                (issue #79), so drag handles and move buttons only appear
                for "manual". Reassigning a task to a different backlog has
                no such conflict and stays available regardless of sort. */}
            {reorderError ? <p className="text-destructive text-sm">{reorderError}</p> : null}
            {selected.size > 0 ? (
              <div className="flex flex-wrap items-center gap-2">
                {assignError ? <span className="text-destructive text-xs">{assignError}</span> : null}
                <span className="text-muted-foreground text-xs">{selected.size} selected</span>
                {/* Named apart from the "Assign to backlog" button next to
                    it, which is the action rather than the picker. */}
                <Combobox
                  aria-label="Backlog to assign"
                  options={assignOptions}
                  value={targetBacklogId}
                  onChange={setTargetBacklogId}
                  size="sm"
                  className="w-44"
                  placeholder="Choose a backlog…"
                  searchPlaceholder="Search backlogs…"
                  emptyText="No backlog found."
                />
                <Button size="sm" onClick={handleAssignSelected} disabled={!targetBacklogId || assigning}>
                  {assigning ? "Assigning…" : "Assign to backlog"}
                </Button>
                <Button variant="outline" size="sm" onClick={() => setSelected(new Set())} disabled={assigning}>
                  Cancel
                </Button>
              </div>
            ) : null}
            <div className="space-y-6">
              {groups.map((group) => {
                const selectable = backlogs.length > 0;
                const manualOrder = sort === "manual";
                return (
                  <div key={group.key}>
                    <h3 className="text-muted-foreground mb-2 text-sm font-medium">
                      {group.name} ({group.tasks.length})
                    </h3>
                    <ul
                      className="space-y-2"
                      onDragOver={(e) => manualOrder && e.preventDefault()}
                      onDrop={(e) => {
                        if (!manualOrder) return;
                        e.preventDefault();
                        handleDropOnTask(group.key, group.tasks, group.tasks.length);
                      }}
                    >
                      {group.tasks.map((task, index) => (
                        <li
                          key={task.id}
                          className="flex items-center gap-2"
                          onDragOver={(e) => manualOrder && e.preventDefault()}
                          onDrop={(e) => {
                            if (!manualOrder) return;
                            e.preventDefault();
                            e.stopPropagation();
                            handleDropOnTask(group.key, group.tasks, index);
                          }}
                        >
                          {selectable ? (
                            <input
                              type="checkbox"
                              aria-label={`Select ${task.title}`}
                              checked={selected.has(task.id)}
                              onChange={() => toggleSelected(task.id)}
                              className="border-input h-4 w-4 shrink-0 rounded"
                            />
                          ) : null}
                          {manualOrder ? (
                            <div className="flex shrink-0 flex-col items-center">
                              <button
                                type="button"
                                aria-label={`Move ${task.title} up`}
                                disabled={index === 0}
                                onClick={() => moveTaskWithinGroup(group.key, group.tasks, index, -1)}
                                className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                              >
                                <ChevronUp className="size-3" />
                              </button>
                              <button
                                type="button"
                                aria-label={`Move ${task.title} down`}
                                disabled={index === group.tasks.length - 1}
                                onClick={() => moveTaskWithinGroup(group.key, group.tasks, index, 1)}
                                className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                              >
                                <ChevronDown className="size-3" />
                              </button>
                            </div>
                          ) : null}
                          {manualOrder ? (
                            <span
                              draggable
                              aria-hidden="true"
                              onDragStart={() => setDraggingTaskId(task.id)}
                              onDragEnd={() => setDraggingTaskId(null)}
                              className="text-muted-foreground shrink-0 cursor-grab active:cursor-grabbing"
                            >
                              <GripVertical className="size-4" />
                            </span>
                          ) : null}
                          <Link
                            href={taskPath(projectId, task.id)}
                            className="border-border hover:border-ring flex flex-1 items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                          >
                            <span className="flex min-w-0 items-center gap-2">
                              <span className="text-foreground truncate">{task.title}</span>
                              {task.labels.length > 0 ? (
                                <span className="flex shrink-0 flex-wrap gap-1">
                                  {task.labels.map((label) => (
                                    <Badge key={label} variant="outline">
                                      {label}
                                    </Badge>
                                  ))}
                                </span>
                              ) : null}
                            </span>
                            <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                              {task.assigneeGitlabUsername ? (
                                <span>{task.assigneeGitlabUsername}</span>
                              ) : null}
                              {task.dueOn ? <span>Due {formatDate(task.dueOn)}</span> : null}
                              <PriorityBadge priority={task.priority} />
                              <ProgressBadge progress={task.progress} />
                              <StatusBadge status={task.status} />
                              <SyncBadge gitlab={task.gitlab} />
                            </span>
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </div>
                );
              })}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
