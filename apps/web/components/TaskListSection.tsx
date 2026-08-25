"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { Plus, X } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { gitlabConnectionPath, taskPath, UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { fromDateParam } from "@/lib/dates";
import { dueStatus } from "@/lib/dashboard";
import { useViewMode } from "@/lib/useViewMode";
import type {
  Backlog,
  Epic,
  Priority,
  Progress,
  Task,
  TaskDependency,
  TaskStatus,
} from "@/types";
import { PROGRESS_COLUMNS } from "@/lib/progress";
import { PRIORITY_COLUMNS } from "@/lib/priority";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { DueDateLabel } from "@/components/DueDateLabel";
import { LabelBadge } from "@/components/LabelBadge";
import { NewTaskForm } from "@/components/NewTaskForm";
import { CreateFormRegion } from "@/components/CreateFormRegion";
import { PriorityBadge } from "@/components/PriorityBadge";
import { SizeBadge } from "@/components/SizeBadge";
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
  () =>
    import("@/components/TaskTimelineSection").then(
      (m) => m.TaskTimelineSection,
    ),
  {
    loading: () => (
      <p className="text-muted-foreground text-sm">Loading timeline…</p>
    ),
  },
);

// "manual" keeps the API's own default (creation) order;
// the rest mirror the sort values the cross-project Task collection accepts
// (issue #76's `?sort=dueOn|priority|updatedAt` on `GET /api/v1/tasks`, see
// AllTasksSection) so the two screens don't disagree on what "sort by
// priority" means.
type TaskSort =
  "manual" | "dueOn" | "priority" | "progress" | "size" | "updatedAt";

/** The `?due=` values (issue #148): a task's dueStatus (lib/dashboard.ts)
 *  narrowed to the three a person would filter by — "later" isn't offered
 *  its own option since it reads the same as no filter at all. */
type DueFilterValue = "overdue" | "dueSoon" | "undated";

function isDueFilterValue(value: string | null): value is DueFilterValue {
  return value === "overdue" || value === "dueSoon" || value === "undated";
}

/** Which bulk action the selection bar is mid-flight on (issue #149) — drives
 *  the pending label on its own button and disables the rest, so two bulk
 *  requests never race each other over the same selection. */
type BulkAction = "assign" | "priority" | "progress" | "close" | "reopen";

/** requestOk resolves a fetch to whether it succeeded, folding a thrown
 *  network error into the same "this one failed" outcome a non-ok response
 *  produces — so a bulk action's per-task Promise.all never rejects outright
 *  and loses track of which of the other tasks came back fine. */
async function requestOk(promise: Promise<Response>): Promise<boolean> {
  try {
    return (await promise).ok;
  } catch {
    return false;
  }
}

/** Whether the "My tasks" filter (issue #146) can actually match anything:
 *  "available" once both a GitLab connection and a matching identity exist,
 *  "no-connection"/"no-identity" name whichever of the two is missing, so
 *  the checkbox can disable itself and point at the fix instead of quietly
 *  returning zero results (?assignee=me matches nothing server-side rather
 *  than erroring — see ListFilter.AssigneeMe, internal/task/task.go). */
export type AssigneeAvailability =
  "available" | "no-connection" | "no-identity";

const UNCLASSIFIED = UNCLASSIFIED_BACKLOG;
const UNCLASSIFIED_LABEL = "Unclassified";

/** The `?epic=` value standing for "tasks in no epic at all" — the tasks
 *  sitting directly in a backlog, which is every task in a project that
 *  hasn't adopted the epic rung. The API spells the same thing "unassigned"
 *  on epic_id. */
export const NO_EPIC = "none";

/** A row shows at most this many labels before rolling the rest into a "+n"
 *  badge (issue #154) — a task with a handful of GitLab labels was crowding
 *  the row's fixed-width meta section on top of assignee/due/the four
 *  status badges. */
const MAX_VISIBLE_ROW_LABELS = 3;

/** The one place that defines what "no filter" means for each URL-held
 *  filter (issue #150) — both the six changeXFilter functions below (which
 *  drop a value back out of the query string once it matches its default)
 *  and the "is any filter active" check that drives the Clear filters
 *  control read from here, rather than each repeating the same literal. */
const FILTER_DEFAULTS = {
  backlog: "all",
  epic: "all",
  status: "open",
  progress: "all",
  priority: "all",
  size: "all",
  due: "all",
  sort: "manual",
} as const;

function StatusBadge({ status }: { status: TaskStatus }) {
  return (
    <Badge variant={status === "open" ? "default" : "secondary"}>
      {status === "open" ? "Open" : "Closed"}
    </Badge>
  );
}

/**
 * SelectAllCheckbox drives one tri-state checkbox against a set of task ids —
 * checked once every id is selected, indeterminate once some but not all are
 * — so selecting "everything visible" or "everything in this backlog" is one
 * click instead of one per row (issue #149). `indeterminate` isn't a DOM
 * attribute React can set via props, so it goes through a ref.
 */
function SelectAllCheckbox({
  label,
  ids,
  selected,
  onChange,
}: {
  label: string;
  ids: string[];
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  const selectedCount = ids.filter((id) => selected.has(id)).length;
  const allSelected = ids.length > 0 && selectedCount === ids.length;
  const indeterminate = selectedCount > 0 && !allSelected;

  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);

  return (
    <input
      ref={ref}
      type="checkbox"
      aria-label={label}
      checked={allSelected}
      disabled={ids.length === 0}
      onChange={() => {
        const next = new Set(selected);
        for (const id of ids) {
          if (allSelected) next.delete(id);
          else next.add(id);
        }
        onChange(next);
      }}
      className="border-input h-4 w-4 shrink-0 rounded disabled:cursor-not-allowed disabled:opacity-50"
    />
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
  epics = [],
  dependencies = [],
  backlogFilter = "all",
  epicFilter = "all",
  search = "",
  statusFilter = "open",
  sort = "manual",
  assigneeMe = false,
  assigneeAvailability = "available",
  today,
  totalCount,
  error = false,
  initialView = "board",
}: {
  projectId: string;
  /** The project's tasks matching the filters below — the API applied them,
   *  so this is what every view mode renders as-is. */
  tasks: Task[];
  backlogs: Backlog[];
  /** The project's epics, for the `?epic=` filter's options and for naming
   *  each task's epic on its row. Empty — the case for a project that
   *  doesn't use the epic rung at all — drops the filter entirely. */
  epics?: Epic[];
  dependencies?: TaskDependency[];
  /** The applied `?backlog=`: a backlog id, UNCLASSIFIED_BACKLOG, or "all" —
   *  how the backlog screens hand off to this collection. */
  backlogFilter?: string;
  /** The applied `?epic=`: an epic id, NO_EPIC, or "all" — how the epic
   *  screens hand off to this collection, the same way the backlog ones do. */
  epicFilter?: string;
  /** The applied `?q=`, if any. Matched server-side against a task's title
   *  and description (`websearch_to_tsquery`), so it matches whole words
   *  rather than any substring. */
  search?: string;
  /** The applied `?status=`. Defaults to "open" so closed tasks don't fill
   *  the list. */
  statusFilter?: "all" | TaskStatus;
  /** The applied `?sort=`, or "manual" for the API's own default order. */
  sort?: TaskSort;
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
  /** The project's task count with no filter applied at all (issue #150) —
   *  the "母数" the result count reads against, e.g. "12 / 340 tasks".
   *  Undefined omits the "/ total" part rather than showing a wrong number,
   *  since the caller (page.tsx) may not always have one to hand. */
  totalCount?: number;
  error?: boolean;
  /** The applied `?view=` (issue #153) — page.tsx already validated it, the
   *  same fallback-to-default treatment every other filter above gets.
   *  Board is the default: progress is the axis people scan the collection
   *  by day to day, so the screen opens there and List/Timeline are one
   *  click away. */
  initialView?: ViewMode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [view, setView] = useViewMode(initialView);
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [targetBacklogId, setTargetBacklogId] = useState("");
  const [bulkPriority, setBulkPriority] = useState<Priority | "">("");
  const [bulkProgress, setBulkProgress] = useState<Progress | "">("");
  const [bulkPending, setBulkPending] = useState<BulkAction | null>(null);
  const [bulkError, setBulkError] = useState<string | null>(null);

  // See the class doc comment: unlike every other filter, `?label=` and
  // `?due=` narrow the list here rather than the API request.
  const labelFilter = searchParams.get("label") ?? undefined;
  const dueParam = searchParams.get("due");
  const dueFilter = isDueFilterValue(dueParam) ? dueParam : undefined;
  // See the `today` prop doc comment for why this isn't `new Date()` here.
  // Memoized on `today` (a stable string) rather than left to run fresh every
  // render, since a bare `new Date()` fallback would otherwise change
  // identity each time and defeat visibleTasks's own memoization below.
  const now = useMemo(() => fromDateParam(today) ?? new Date(), [today]);
  const visibleTasks = useMemo(() => {
    let result = labelFilter
      ? tasks.filter((t) => t.labels.includes(labelFilter))
      : tasks;
    if (dueFilter) {
      result = result.filter((t) => dueStatus(t.dueOn, now) === dueFilter);
    }
    return result;
  }, [tasks, labelFilter, dueFilter, now]);

  // A task selected under one filter can fall out of view under the next —
  // prune it from the selection rather than leaving an invisible task as the
  // target of the next bulk action (issue #149).
  useEffect(() => {
    setSelected((prev) => {
      if (prev.size === 0) return prev;
      const visibleIds = new Set(visibleTasks.map((t) => t.id));
      const next = new Set([...prev].filter((id) => visibleIds.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [visibleTasks]);

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

  // The epic filter's own options, the same shape: every epic, plus "all" and
  // the tasks sitting directly in a backlog — which is every task in a
  // project that hasn't adopted the epic rung, hence its own option rather
  // than being folded into "all".
  const epicFilterOptions = useMemo(
    () => [
      { value: "all", label: "All epics" },
      ...epics.map((e) => ({ value: e.id, label: e.name })),
      { value: NO_EPIC, label: "No epic" },
    ],
    [epics],
  );

  // Each task's epic name, for the row/card labels below.
  const epicNames = useMemo(
    () => new Map(epics.map((e) => [e.id, e.name])),
    [epics],
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
      if (list) {
        ordered.push({ key: backlog.id, name: backlog.name, tasks: list });
      }
    }
    const unclassified = byBacklog.get(UNCLASSIFIED);
    if (unclassified) {
      ordered.push({
        key: UNCLASSIFIED,
        name: UNCLASSIFIED_LABEL,
        tasks: unclassified,
      });
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
    updateQuery({
      backlog: value === FILTER_DEFAULTS.backlog ? undefined : value,
    });
  }

  function changeEpicFilter(value: string) {
    updateQuery({ epic: value === FILTER_DEFAULTS.epic ? undefined : value });
  }

  function changeStatusFilter(value: "all" | TaskStatus) {
    updateQuery({
      status: value === FILTER_DEFAULTS.status ? undefined : value,
    });
  }

  function changeAssigneeMe(checked: boolean) {
    updateQuery({ assignee: checked ? "me" : undefined });
  }

  function changeDueFilter(value: string) {
    updateQuery({ due: value === FILTER_DEFAULTS.due ? undefined : value });
  }

  function changeSearch(value: string) {
    updateQuery({ q: value.trim() === "" ? undefined : value });
  }

  function changeSort(value: TaskSort) {
    updateQuery({ sort: value === FILTER_DEFAULTS.sort ? undefined : value });
  }

  function toggleLabelFilter(label: string) {
    updateQuery({ label: labelFilter === label ? undefined : label });
  }

  // Drives the Clear filters control (issue #150): true once any filter
  // disagrees with FILTER_DEFAULTS (or, for the two with no "all" sentinel,
  // its own empty default). Sort is included since a non-manual sort is as
  // much a reason today's result differs from tomorrow's default view as any
  // other filter is.
  const hasActiveFilters =
    backlogFilter !== FILTER_DEFAULTS.backlog ||
    epicFilter !== FILTER_DEFAULTS.epic ||
    search.trim() !== "" ||
    statusFilter !== FILTER_DEFAULTS.status ||
    assigneeMe ||
    sort !== FILTER_DEFAULTS.sort ||
    Boolean(labelFilter) ||
    Boolean(dueFilter);

  // Every filter above is a URL query parameter, so resetting all of them at
  // once is just dropping the query string entirely rather than undoing each
  // control in turn.
  function clearFilters() {
    router.push(pathname);
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
    if (epicFilter !== "all") {
      const label =
        epicFilter === NO_EPIC
          ? "no epic"
          : (epics.find((e) => e.id === epicFilter)?.name ?? "this epic");
      const statusPart = statusFilter === "all" ? "" : `${statusFilter} `;
      return `No ${statusPart}tasks in ${label}.`;
    }
    if (backlogFilter !== "all") {
      const label =
        filterOptions.find((o) => o.value === backlogFilter)?.label ??
        "this backlog";
      const statusPart = statusFilter === "all" ? "" : `${statusFilter} `;
      return `No ${statusPart}tasks in ${label}.`;
    }
    if (statusFilter !== "all") {
      return `No ${statusFilter} tasks.`;
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

  /**
   * runBulkAction fires one request per selected task and folds the results
   * into a single outcome: full success clears the selection, a partial
   * failure reports how many of how many failed (issue #149) and narrows the
   * selection down to just those, so retrying only resends to the tasks that
   * actually need it.
   */
  async function runBulkAction(
    action: BulkAction,
    ids: string[],
    request: (taskId: string) => Promise<Response>,
  ): Promise<string[]> {
    setBulkPending(action);
    setBulkError(null);
    try {
      const results = await Promise.all(
        ids.map(async (taskId) => ({
          taskId,
          ok: await requestOk(request(taskId)),
        })),
      );
      const failed = results.filter((r) => !r.ok).map((r) => r.taskId);
      if (failed.length > 0) {
        setBulkError(
          failed.length === ids.length
            ? `Failed to update ${ids.length} task${ids.length === 1 ? "" : "s"}.`
            : `${failed.length} of ${ids.length} tasks failed to update.`,
        );
        setSelected(new Set(failed));
      } else {
        setSelected(new Set());
      }
      router.refresh();
      return failed;
    } finally {
      setBulkPending(null);
    }
  }

  async function handleAssignSelected() {
    if (!targetBacklogId || selected.size === 0) return;
    const backlogId = targetBacklogId === UNCLASSIFIED ? null : targetBacklogId;
    const failed = await runBulkAction(
      "assign",
      Array.from(selected),
      (taskId) =>
        fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/assign-backlog`, {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({ backlogId }),
        }),
    );
    if (failed.length === 0) setTargetBacklogId("");
  }

  async function handleBulkPriority() {
    if (!bulkPriority || selected.size === 0) return;
    const priority = bulkPriority;
    const failed = await runBulkAction(
      "priority",
      Array.from(selected),
      (taskId) =>
        fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}`, {
          method: "PATCH",
          credentials: "include",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({ priority }),
        }),
    );
    if (failed.length === 0) setBulkPriority("");
  }

  async function handleBulkProgress() {
    if (!bulkProgress || selected.size === 0) return;
    const progress = bulkProgress;
    const failed = await runBulkAction(
      "progress",
      Array.from(selected),
      (taskId) =>
        fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}`, {
          method: "PATCH",
          credentials: "include",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({ progress }),
        }),
    );
    if (failed.length === 0) setBulkProgress("");
  }

  async function handleBulkClose() {
    if (selected.size === 0) return;
    await runBulkAction("close", Array.from(selected), (taskId) =>
      fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/close`, {
        method: "POST",
        credentials: "include",
        headers: csrfHeaders(),
      }),
    );
  }

  async function handleBulkReopen() {
    if (selected.size === 0) return;
    await runBulkAction("reopen", Array.from(selected), (taskId) =>
      fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/reopen`, {
        method: "POST",
        credentials: "include",
        headers: csrfHeaders(),
      }),
    );
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
            <div className="flex flex-wrap items-baseline gap-2">
              <CardTitle className="text-base font-medium">Tasks</CardTitle>
              {/* The result count (issue #150): what's showing under the
                  current filters against the project's total with none
                  applied — stays put across Board/List/Timeline since it
                  lives here, above the view branch, rather than inside any
                  one of them. Left off entirely on a load error, alongside
                  the filter row below. */}
              {!error ? (
                <span className="text-muted-foreground text-xs">
                  {`${visibleTasks.length}${totalCount !== undefined ? ` / ${totalCount}` : ""} tasks`}
                </span>
              ) : null}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {/* The view toggle stays put whatever the filters return, for the
                  same reason the filter row below does — a view mode is not
                  something to be stuck in because the current filter came back
                  empty. "New task" stays reachable even on a load error. */}
              {!error ? (
                <ViewModeToggle value={view} onChange={setView} />
              ) : null}
              {!creating ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setCreating(true)}
                >
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
                onValueChange={(value) =>
                  changeStatusFilter(value as "all" | TaskStatus)
                }
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
                value={dueFilter ?? "all"}
                onValueChange={changeDueFilter}
              >
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
              {/* Only offered once the project actually uses the epic rung:
                  it is optional, and a filter with nothing behind it is just
                  a question the screen can't answer. */}
              {epics.length > 0 ? (
                <Combobox
                  aria-label="Epic"
                  options={epicFilterOptions}
                  value={epicFilter}
                  onChange={changeEpicFilter}
                  size="sm"
                  className="w-44"
                  searchPlaceholder="Search epics…"
                  emptyText="No epic found."
                />
              ) : null}
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
              <Select
                value={sort}
                onValueChange={(value) => changeSort(value as TaskSort)}
              >
                <SelectTrigger size="sm" aria-label="Sort" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="manual">Default order</SelectItem>
                  <SelectItem value="dueOn">Due date</SelectItem>
                  <SelectItem value="priority">Priority</SelectItem>
                  <SelectItem value="progress">Progress</SelectItem>
                  <SelectItem value="size">Size</SelectItem>
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
                <Link
                  href="/settings"
                  className="text-muted-foreground hover:text-foreground text-xs underline"
                >
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
              {/* Only appears once a filter actually differs from
                  FILTER_DEFAULTS (issue #150) — otherwise there is nothing
                  for it to undo, and a control that's always there but
                  usually a no-op just adds clutter to the row. */}
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
            <NewTaskForm
              projectId={projectId}
              backlogs={backlogs}
              epics={epics}
              defaultBacklogId={
                backlogFilter !== "all" && backlogFilter !== UNCLASSIFIED
                  ? backlogFilter
                  : null
              }
              defaultEpicId={
                epicFilter !== "all" && epicFilter !== NO_EPIC
                  ? epicFilter
                  : null
              }
              onCancel={() => setCreating(false)}
            />
          </CreateFormRegion>
        ) : null}
        {error ? (
          <p className="text-destructive text-sm">
            Failed to load tasks. Try refreshing the page.
          </p>
        ) : visibleTasks.length === 0 ? (
          // Checked before the view branch so the timeline doesn't answer an
          // empty filter result with its own "set a start or due date" hint.
          <p className="text-muted-foreground text-sm">
            {emptyFilterMessage()}
          </p>
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
            {/* Selection itself no longer needs a backlog to exist (issue
                #149) — it's the "Assign to backlog" action further down that
                stays conditional on one. A top-level select-all covers the
                whole filtered result, not just what's scrolled into view. */}
            <div className="flex items-center gap-2">
              <SelectAllCheckbox
                label="Select all tasks"
                ids={visibleTasks.map((t) => t.id)}
                selected={selected}
                onChange={setSelected}
              />
              <span className="text-muted-foreground text-xs">Select all</span>
            </div>
            {selected.size > 0 ? (
              <div className="flex flex-wrap items-center gap-2">
                {bulkError ? (
                  <span className="text-destructive text-xs">{bulkError}</span>
                ) : null}
                <span className="text-muted-foreground text-xs">
                  {selected.size} selected
                </span>
                {backlogs.length > 0 ? (
                  <>
                    {/* Named apart from the "Assign to backlog" button next
                        to it, which is the action rather than the picker. */}
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
                    <Button
                      size="sm"
                      onClick={handleAssignSelected}
                      disabled={!targetBacklogId || bulkPending !== null}
                    >
                      {bulkPending === "assign"
                        ? "Assigning…"
                        : "Assign to backlog"}
                    </Button>
                  </>
                ) : null}
                <Select
                  value={bulkPriority}
                  onValueChange={(value) => setBulkPriority(value as Priority)}
                >
                  <SelectTrigger
                    size="sm"
                    aria-label="Priority to set"
                    className="w-36"
                  >
                    <SelectValue placeholder="Set priority…" />
                  </SelectTrigger>
                  <SelectContent>
                    {PRIORITY_COLUMNS.map((option) => (
                      <SelectItem key={option.priority} value={option.priority}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  size="sm"
                  onClick={handleBulkPriority}
                  disabled={!bulkPriority || bulkPending !== null}
                >
                  {bulkPending === "priority" ? "Setting…" : "Set priority"}
                </Button>
                <Select
                  value={bulkProgress}
                  onValueChange={(value) => setBulkProgress(value as Progress)}
                >
                  <SelectTrigger
                    size="sm"
                    aria-label="Progress to set"
                    className="w-36"
                  >
                    <SelectValue placeholder="Set progress…" />
                  </SelectTrigger>
                  <SelectContent>
                    {PROGRESS_COLUMNS.map((option) => (
                      <SelectItem key={option.progress} value={option.progress}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  size="sm"
                  onClick={handleBulkProgress}
                  disabled={!bulkProgress || bulkPending !== null}
                >
                  {bulkPending === "progress" ? "Setting…" : "Set progress"}
                </Button>
                {/* Both stay offered regardless of the selection's current
                    mix of open/closed tasks — closing an already-closed task
                    (or reopening an already-open one) is a no-op server-side,
                    the same as the single-task CloseReopenButton relies on. */}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleBulkClose}
                  disabled={bulkPending !== null}
                >
                  {bulkPending === "close" ? "Closing…" : "Close selected"}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleBulkReopen}
                  disabled={bulkPending !== null}
                >
                  {bulkPending === "reopen" ? "Reopening…" : "Reopen selected"}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSelected(new Set())}
                  disabled={bulkPending !== null}
                >
                  Cancel
                </Button>
              </div>
            ) : null}
            <div className="space-y-6">
              {groups.map((group) => {
                return (
                  <div key={group.key}>
                    <h3 className="text-muted-foreground mb-2 flex items-center gap-2 text-sm font-medium">
                      <SelectAllCheckbox
                        label={`Select all in ${group.name}`}
                        ids={group.tasks.map((t) => t.id)}
                        selected={selected}
                        onChange={setSelected}
                      />
                      {group.name} ({group.tasks.length})
                    </h3>
                    <ul className="space-y-2">
                      {group.tasks.map((task) => {
                        return (
                          <li key={task.id} className="flex items-center gap-2">
                            <input
                              type="checkbox"
                              aria-label={`Select ${task.title}`}
                              checked={selected.has(task.id)}
                              onChange={() => toggleSelected(task.id)}
                              className="border-input h-4 w-4 shrink-0 rounded"
                            />
                            <Link
                              href={taskPath(projectId, task.id)}
                              className="border-border hover:border-ring flex flex-1 flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-md border px-3 py-2 text-sm transition-colors"
                            >
                              <span className="flex min-w-0 items-center gap-2">
                                <span className="text-foreground truncate">
                                  {task.title}
                                </span>
                                {task.labels.length > 0 ? (
                                  <span className="flex shrink-0 flex-wrap gap-1">
                                    {task.labels
                                      .slice(0, MAX_VISIBLE_ROW_LABELS)
                                      .map((label) => (
                                        <LabelBadge
                                          key={label}
                                          label={label}
                                          active={label === labelFilter}
                                          onToggle={toggleLabelFilter}
                                        />
                                      ))}
                                    {task.labels.length >
                                    MAX_VISIBLE_ROW_LABELS ? (
                                      <Badge
                                        variant="outline"
                                        title={task.labels
                                          .slice(MAX_VISIBLE_ROW_LABELS)
                                          .join(", ")}
                                      >
                                        {`+${task.labels.length - MAX_VISIBLE_ROW_LABELS}`}
                                      </Badge>
                                    ) : null}
                                  </span>
                                ) : null}
                              </span>
                              <span className="text-muted-foreground flex w-full flex-wrap items-center gap-3 text-xs sm:w-auto sm:flex-nowrap">
                                {/* The rows are grouped by backlog, so the
                                      backlog is already stated by the group
                                      header above — the epic is the rung it
                                      doesn't say, and one backlog group
                                      routinely holds several. */}
                                {task.epicId && epicNames.has(task.epicId) ? (
                                  <span className="truncate">
                                    {epicNames.get(task.epicId)}
                                  </span>
                                ) : null}
                                {task.assigneeGitlabUsername ? (
                                  <span>{task.assigneeGitlabUsername}</span>
                                ) : null}
                                {task.dueOn ? (
                                  <DueDateLabel dueOn={task.dueOn} now={now} />
                                ) : null}
                                <PriorityBadge priority={task.priority} />
                                <ProgressBadge progress={task.progress} />
                                <SizeBadge size={task.size} />
                                {/* Closed is the one status worth a badge —
                                      open is the default nearly every task is
                                      in, so stating it on every row was pure
                                      noise (issue #154). */}
                                {task.status === "closed" ? (
                                  <StatusBadge status={task.status} />
                                ) : null}
                                <SyncBadge gitlab={task.gitlab} />
                              </span>
                            </Link>
                          </li>
                        );
                      })}
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
