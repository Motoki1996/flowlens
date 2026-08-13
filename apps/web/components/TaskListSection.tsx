"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { ChevronDown, ChevronUp, GripVertical, Plus, X } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { gitlabConnectionPath, taskPath, UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { fromDateParam, toApiDate } from "@/lib/dates";
import { dueStatus } from "@/lib/dashboard";
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
import { PRIORITY_COLUMNS, PRIORITY_LABELS } from "@/lib/priority";
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
import { DueDateLabel } from "@/components/DueDateLabel";
import { LabelBadge } from "@/components/LabelBadge";
import { PriorityBadge } from "@/components/PriorityBadge";
import { ProgressBadge } from "@/components/ProgressBadge";
import { SyncBadge } from "@/components/SyncBadge";
import { TaskBoardSection } from "@/components/TaskBoardSection";
import { TaskSearchBox } from "@/components/TaskSearchBox";
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

/** The `?due=` values (issue #148): a task's dueStatus (lib/dashboard.ts)
 *  narrowed to the three a person would filter by — "later" isn't offered
 *  its own option since it reads the same as no filter at all. */
type DueFilterValue = "overdue" | "dueSoon" | "undated";

function isDueFilterValue(value: string | null): value is DueFilterValue {
  return value === "overdue" || value === "dueSoon" || value === "undated";
}

/** Whether the "My tasks" filter (issue #146) can actually match anything:
 *  "available" once both a GitLab connection and a matching identity exist,
 *  "no-connection"/"no-identity" name whichever of the two is missing, so
 *  the checkbox can disable itself and point at the fix instead of quietly
 *  returning zero results (?assignee=me matches nothing server-side rather
 *  than erroring — see ListFilter.AssigneeMe, internal/task/task.go). */
export type AssigneeAvailability = "available" | "no-connection" | "no-identity";

const UNCLASSIFIED = UNCLASSIFIED_BACKLOG;
const UNCLASSIFIED_LABEL = "Unclassified";

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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
            {PRIORITY_COLUMNS.map((option) => (
              <SelectItem key={option.priority} value={option.priority}>
                {option.label}
              </SelectItem>
            ))}
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
 * TaskListSection is the Task collection at /projects/[projectId]/tasks (no
 * standalone "unclassified" screen), opening in the Board view mode. In its
 * List mode tasks are grouped by backlog, with a trailing Unclassified group
 * for tasks that have no backlog. Filters narrow every mode alike.
 *
 * The filters themselves are the URL, not local state (issue #143): `tasks`
 * arrives already narrowed and ordered by the API, and changing a filter
 * pushes a new query string for the server component above to re-fetch with
 * — the same round trip the cross-project collection (AllTasksSection)
 * makes. Only the view mode, the optimistic drag order, and the label filter
 * stay client-side.
 *
 * `?label=` (issue #147) and `?due=` (issue #148) are the two filters the API
 * doesn't apply: the task list is already fetched in full, so narrowing by
 * either costs nothing to do client-side — labels because `tasks.labels`
 * being a `text[]` would need a schema-aware query to filter server-side, due
 * date because the classification itself (`lib/dashboard.ts`'s `dueStatus`)
 * is app-only reasoning the API has no reason to know. Both still live in the
 * URL for shareability and reload, read straight from `useSearchParams`
 * rather than threaded through page.tsx as a prop, since nothing server-side
 * ever needs to know either value — `today` (below) is the one date input
 * that *does* come from page.tsx, since the classification's "today" cutoff
 * itself has to be.
 */
export function TaskListSection({
  projectId,
  tasks,
  backlogs,
  dependencies = [],
  backlogFilter = "all",
  search = "",
  statusFilter = "open",
  sort = "manual",
  progressFilter,
  priorityFilter,
  assigneeMe = false,
  assigneeAvailability = "available",
  today,
  error = false,
}: {
  projectId: string;
  /** The project's tasks matching the filters below — the API applied them,
   *  so this is what every view mode renders as-is. */
  tasks: Task[];
  backlogs: Backlog[];
  dependencies?: TaskDependency[];
  /** The applied `?backlog=`: a backlog id, UNCLASSIFIED_BACKLOG, or "all" —
   *  how the backlog screens hand off to this collection. */
  backlogFilter?: string;
  /** The applied `?q=`, if any. Matched server-side against a task's title
   *  and description (`websearch_to_tsquery`), so it matches whole words
   *  rather than any substring. */
  search?: string;
  /** The applied `?status=`. Defaults to "open" so closed tasks don't fill
   *  the list. */
  statusFilter?: "all" | TaskStatus;
  /** The applied `?sort=`, or "manual" for the API's own position order. */
  sort?: TaskSort;
  /** The applied `?progress=`; undefined means all of them — unlike status,
   *  no progress stage is noise worth hiding by default. */
  progressFilter?: Progress;
  /** The applied `?priority=`; undefined means all of them, same as progress. */
  priorityFilter?: Priority;
  /** True when `?assignee=me` is set (issue #146, extending issue #102's
   *  cross-project toggle to this project-scoped collection): only tasks
   *  assigned to the caller's own registered GitLab identity. Held in the
   *  URL like the other filters above. */
  assigneeMe?: boolean;
  /** Whether "My tasks" can match anything for this project/caller — see
   *  AssigneeAvailability. Defaults to "available" so callers that don't
   *  care about this (most tests) get a plain, enabled checkbox. */
  assigneeAvailability?: AssigneeAvailability;
  /** The server's current date as `YYYY-MM-DD` (`toDateParam(new Date())`,
   *  page.tsx) — the "today" cutoff the `?due=` filter and each row's/card's
   *  DueDateLabel classify against (issue #148). Passed down rather than
   *  computed here with `new Date()`: this is a client component, so its
   *  initial render also runs server-side, and computing "now" independently
   *  there and again during hydration risks disagreeing under a server/
   *  browser timezone difference — which React reports as a hydration
   *  mismatch. Falls back to the browser's own `new Date()` when absent
   *  (every real caller passes it; only tests that don't care rely on the
   *  fallback). */
  today?: string;
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  // Board is the default: progress is the axis people scan the collection by
  // day to day, so the screen opens there and List/Timeline are one click away.
  const [view, setView] = useState<ViewMode>("board");
  const [creating, setCreating] = useState(false);
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

  // See the class doc comment: unlike every other filter, `?label=` and
  // `?due=` narrow `localTasks` here rather than the API request.
  const labelFilter = searchParams.get("label") ?? undefined;
  const dueParam = searchParams.get("due");
  const dueFilter = isDueFilterValue(dueParam) ? dueParam : undefined;
  // See the `today` prop doc comment for why this isn't `new Date()` here.
  // Memoized on `today` (a stable string) rather than left to run fresh every
  // render, since a bare `new Date()` fallback would otherwise change
  // identity each time and defeat visibleTasks's own memoization below.
  const now = useMemo(() => fromDateParam(today) ?? new Date(), [today]);
  const visibleTasks = useMemo(() => {
    let result = labelFilter ? localTasks.filter((t) => t.labels.includes(labelFilter)) : localTasks;
    if (dueFilter) {
      result = result.filter((t) => dueStatus(t.dueOn, now) === dueFilter);
    }
    return result;
  }, [localTasks, labelFilter, dueFilter, now]);

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

  const groups = useMemo(() => {
    const byBacklog = new Map<string, Task[]>();
    for (const t of visibleTasks) {
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
  }, [visibleTasks, backlogs]);

  /**
   * Every filter/sort choice belongs in the URL: the screen stays shareable,
   * the browser's back button walks the filter history, and — since the API
   * is what applies them (issue #143) — the push is also what re-fetches the
   * list. A value equal to that filter's default drops out of the query
   * string rather than being spelled out.
   */
  function updateQuery(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(next)) {
      params.delete(key);
      if (value) params.set(key, value);
    }
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  function changeBacklogFilter(value: string) {
    updateQuery({ backlog: value === "all" ? undefined : value });
  }

  function changeStatusFilter(value: "all" | TaskStatus) {
    updateQuery({ status: value === "open" ? undefined : value });
  }

  function changeProgressFilter(value: "all" | Progress) {
    updateQuery({ progress: value === "all" ? undefined : value });
  }

  function changePriorityFilter(value: "all" | Priority) {
    updateQuery({ priority: value === "all" ? undefined : value });
  }

  function changeAssigneeMe(checked: boolean) {
    updateQuery({ assignee: checked ? "me" : undefined });
  }

  function changeDueFilter(value: string) {
    updateQuery({ due: value === "all" ? undefined : value });
  }

  function changeSearch(value: string) {
    updateQuery({ q: value.trim() === "" ? undefined : value });
  }

  function changeSort(value: TaskSort) {
    updateQuery({ sort: value === "manual" ? undefined : value });
  }

  function toggleLabelFilter(label: string) {
    updateQuery({ label: labelFilter === label ? undefined : label });
  }

  // Distinguishes *why* the list came back empty — a bare "no matches" reads
  // the same whether it's the search term, the status default hiding every
  // closed task, or the backlog picker, so each gets its own wording. The
  // status filter is always on (defaulting to open), so this also stands in
  // for the old "No tasks yet.": with the filtering server-side there is no
  // second, unfiltered list to tell an empty project apart from an empty
  // result — and "No open tasks." is true of both.
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
    if (progressFilter) {
      return `No ${PROGRESS_LABELS[progressFilter].toLowerCase()} tasks.`;
    }
    if (priorityFilter) {
      return `No ${PRIORITY_LABELS[priorityFilter].toLowerCase()} priority tasks.`;
    }
    if (dueFilter === "overdue") {
      return "No overdue tasks.";
    }
    if (dueFilter === "dueSoon") {
      return "No tasks due this week.";
    }
    if (dueFilter === "undated") {
      return "No tasks without a due date.";
    }
    if (assigneeMe) {
      return "No tasks are assigned to you.";
    }
    if (labelFilter) {
      return `No tasks labeled "${labelFilter}".`;
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
            headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
              {/* The view toggle stays put whatever the filters return, for the
                  same reason the filter row below does — a view mode is not
                  something to be stuck in because the current filter came back
                  empty. "New task" stays reachable even on a load error. */}
              {!error ? <ViewModeToggle value={view} onChange={setView} /> : null}
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
              with the project and get the searchable Combobox.

              They stay mounted whatever comes back, too: now that they narrow
              the API request itself, hiding them on an empty result would be
              hiding the only way back out of the filter that emptied it. */}
          {!error ? (
            <div className="flex flex-wrap items-center gap-2">
              <TaskSearchBox value={search} onChange={changeSearch} />
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
                value={progressFilter ?? "all"}
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
              <Select
                value={priorityFilter ?? "all"}
                onValueChange={(value) => changePriorityFilter(value as "all" | Priority)}
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
              <Select value={dueFilter ?? "all"} onValueChange={changeDueFilter}>
                <SelectTrigger size="sm" aria-label="Due date" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All due dates</SelectItem>
                  <SelectItem value="overdue">Overdue</SelectItem>
                  <SelectItem value="dueSoon">Due this week</SelectItem>
                  <SelectItem value="undated">No due date</SelectItem>
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
              <label
                className="text-muted-foreground flex items-center gap-1.5 text-sm"
                title={
                  assigneeAvailability === "no-connection"
                    ? "Connect this project to GitLab to filter by assignee."
                    : assigneeAvailability === "no-identity"
                      ? "Register your GitLab identity to filter by assignee."
                      : undefined
                }
              >
                <input
                  type="checkbox"
                  checked={assigneeMe}
                  disabled={assigneeAvailability !== "available"}
                  onChange={(e) => changeAssigneeMe(e.target.checked)}
                  className="border-input h-4 w-4 rounded disabled:cursor-not-allowed disabled:opacity-50"
                />
                My tasks
              </label>
              {assigneeAvailability === "no-connection" ? (
                <Link
                  href={gitlabConnectionPath(projectId)}
                  className="text-muted-foreground hover:text-foreground text-xs underline"
                >
                  Connect GitLab
                </Link>
              ) : assigneeAvailability === "no-identity" ? (
                <Link href="/settings" className="text-muted-foreground hover:text-foreground text-xs underline">
                  Register GitLab identity
                </Link>
              ) : null}
              {/* The only visible sign a label filter is active — clicking a
                  label badge sets it, but has nowhere else to show it's on,
                  or a way to turn it back off (issue #147). */}
              {labelFilter ? (
                <span className="border-border bg-muted/40 text-muted-foreground flex items-center gap-1 rounded-full border py-0.5 pr-1 pl-2 text-xs">
                  {`Label: ${labelFilter}`}
                  <button
                    type="button"
                    aria-label={`Clear label filter: ${labelFilter}`}
                    onClick={() => toggleLabelFilter(labelFilter)}
                    className="hover:text-foreground rounded-full p-0.5"
                  >
                    <X className="size-3" aria-hidden />
                  </button>
                </span>
              ) : null}
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
        ) : visibleTasks.length === 0 ? (
          // Checked before the view branch so the timeline doesn't answer an
          // empty filter result with its own "set a start or due date" hint.
          <p className="text-muted-foreground text-sm">{emptyFilterMessage()}</p>
        ) : view === "board" ? (
          <TaskBoardSection
            projectId={projectId}
            tasks={visibleTasks}
            backlogs={backlogs}
            activeLabel={labelFilter}
            onLabelClick={toggleLabelFilter}
            now={now}
          />
        ) : view === "timeline" ? (
          <TaskTimelineSection
            projectId={projectId}
            tasks={visibleTasks}
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
                                    <LabelBadge
                                      key={label}
                                      label={label}
                                      active={label === labelFilter}
                                      onToggle={toggleLabelFilter}
                                    />
                                  ))}
                                </span>
                              ) : null}
                            </span>
                            <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                              {task.assigneeGitlabUsername ? (
                                <span>{task.assigneeGitlabUsername}</span>
                              ) : null}
                              {task.dueOn ? <DueDateLabel dueOn={task.dueOn} now={now} /> : null}
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
