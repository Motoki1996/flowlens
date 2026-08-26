"use client";

import { useMemo, useState, type FormEvent } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { Plus } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { backlogPath, tasksPath, UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { toApiDate } from "@/lib/dates";
import { backlogScheduleLabel } from "@/lib/backlogs";
import { backlogTaskCompletion } from "@/lib/timeline";
import { useViewMode } from "@/lib/useViewMode";
import type {
  ApiError,
  Backlog,
  LinkedGitlabProject,
  Priority,
  Progress,
  StatusFilter,
} from "@/types";
import { ClosedBadge } from "@/components/ClosedBadge";
import { PROGRESS_COLUMNS, PROGRESS_LABELS } from "@/lib/progress";
import { PRIORITY_LABELS, PRIORITY_OPTIONS } from "@/lib/priority";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CreateFormRegion } from "@/components/CreateFormRegion";
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
import {
  BacklogEditForm,
  LinkedGitlabProjectField,
} from "@/components/BacklogEditForm";
import { BacklogDeleteButton } from "@/components/BacklogDeleteButton";
import { PriorityBadge, PriorityDot } from "@/components/PriorityBadge";
import { ProgressBadge, ProgressDot } from "@/components/ProgressBadge";
import { TruncatedName } from "@/components/TruncatedName";
import { BacklogBoardSection } from "@/components/BacklogBoardSection";
import { TaskSearchBox } from "@/components/TaskSearchBox";
import { ViewModeToggle, type ViewMode } from "@/components/ViewModeToggle";
import {
  RowCheckbox,
  SelectAllCheckbox,
  useBulkSelection,
  type BaseBulkAction,
} from "@/components/BulkSelection";
import { BulkActionBar } from "@/components/BulkActionBar";

/** The sort values the Backlog collection's `?sort=` accepts (issue #151):
 *  "manual" keeps the API's own default (creation) order;
 *  "priority"/"progress" are applied server-side, the same as the Task
 *  collection's own sort (parseBacklogListFilter,
 *  internal/http/backlog_handler.go); "dueOn" is a Backlog-only value the API
 *  has no concept of (a backlog's schedule is app-only, docs/ui-design.md),
 *  so it's sorted client-side instead — see `visibleBacklogs` below. */
type BacklogSort = "manual" | "dueOn" | "priority" | "progress";

/** compareByDueOn orders backlogs by dueOn ascending, with undated backlogs
 *  last — the client-side half of `sort=dueOn` (see BacklogSort). */
function compareByDueOn(a: Backlog, b: Backlog): number {
  if (!a.dueOn && !b.dueOn) return 0;
  if (!a.dueOn) return 1;
  if (!b.dueOn) return -1;
  return a.dueOn.localeCompare(b.dueOn);
}

/**
 * The Timeline view mode pulls in the charting library, which the default List
 * mode has no use for — loading it on demand keeps that cost off the backlog
 * collection until someone actually switches views. Same arrangement as the
 * Task collection.
 */
const BacklogTimelineSection = dynamic(
  () =>
    import("@/components/BacklogTimelineSection").then(
      (m) => m.BacklogTimelineSection,
    ),
  {
    loading: () => (
      <p className="text-muted-foreground text-sm">Loading timeline…</p>
    ),
  },
);

/** The Select value standing in for "no link of this backlog's own" — Radix
 *  Select has no empty-string item, and the API's own spelling for it is
 *  `null`, which a Select can't hold either. */
/** NewBacklogForm is the inline creation form shown in the backlog list. */
function NewBacklogForm({
  projectId,
  links,
  onCancel,
}: {
  projectId: string;
  links: LinkedGitlabProject[];
  onCancel: () => void;
}) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [startDate, setStartDate] = useState<Date | undefined>(undefined);
  const [dueOn, setDueOn] = useState<Date | undefined>(undefined);
  const [priority, setPriority] = useState<Priority>("medium");
  const [progress, setProgress] = useState<Progress>("not_started");
  const [linkId, setLinkId] = useState<string | null>(null);
  const [baseBranch, setBaseBranch] = useState("");
  const [allowedScope, setAllowedScope] = useState("");
  const [forbiddenScope, setForbiddenScope] = useState("");
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
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/projects/${projectId}/backlogs`,
        {
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
            defaultLinkedGitlabProjectId: linkId,
            baseBranch,
            allowedScope,
            forbiddenScope,
          }),
        },
      );
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
    <form
      onSubmit={handleSubmit}
      className="space-y-3"
      aria-label="New backlog"
    >
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label
          htmlFor="new-backlog-name"
          className="text-foreground block text-sm font-medium"
        >
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
        <label
          htmlFor="new-backlog-description"
          className="text-foreground block text-sm font-medium"
        >
          Description
        </label>
        <Textarea
          id="new-backlog-description"
          name="description"
          aria-describedby="new-backlog-description-hint"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1 min-h-[150px]"
        />
        <p
          id="new-backlog-description-hint"
          className="text-muted-foreground mt-1 text-xs"
        >
          Markdown supported — pasted URLs become links.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <DateField
          id="new-backlog-start-date"
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField
          id="new-backlog-due-on"
          label="Due date"
          value={dueOn}
          onChange={setDueOn}
        />
      </div>
      <div>
        <label
          htmlFor="new-backlog-priority"
          className="text-foreground block text-sm font-medium"
        >
          Priority
        </label>
        <Select
          value={priority}
          onValueChange={(value) => setPriority(value as Priority)}
        >
          <SelectTrigger
            id="new-backlog-priority"
            className="mt-1 w-full sm:w-40"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PRIORITY_OPTIONS.map((option) => (
              <SelectItem key={option.priority} value={option.priority}>
                <PriorityDot priority={option.priority} />
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div>
        <label
          htmlFor="new-backlog-progress"
          className="text-foreground block text-sm font-medium"
        >
          Progress
        </label>
        <Select
          value={progress}
          onValueChange={(value) => setProgress(value as Progress)}
        >
          <SelectTrigger
            id="new-backlog-progress"
            className="mt-1 w-full sm:w-40"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {PROGRESS_COLUMNS.map((option) => (
              <SelectItem key={option.progress} value={option.progress}>
                <ProgressDot progress={option.progress} />
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <LinkedGitlabProjectField
        id="new-backlog-linked-gitlab-project"
        links={links}
        value={linkId}
        onChange={setLinkId}
      />
      <div>
        <label
          htmlFor="new-backlog-base-branch"
          className="text-foreground block text-sm font-medium"
        >
          Base branch
        </label>
        <Input
          id="new-backlog-base-branch"
          name="baseBranch"
          value={baseBranch}
          onChange={(e) => setBaseBranch(e.target.value)}
          placeholder="main"
          className="mt-1 sm:w-80"
        />
        <p className="text-muted-foreground mt-1 text-xs">
          Optional. The branch tasks in this backlog are meant to branch from.
        </p>
      </div>
      <div>
        <label
          htmlFor="new-backlog-allowed-scope"
          className="text-foreground block text-sm font-medium"
        >
          Allowed scope
        </label>
        <Textarea
          id="new-backlog-allowed-scope"
          name="allowedScope"
          value={allowedScope}
          onChange={(e) => setAllowedScope(e.target.value)}
          rows={3}
          className="mt-1"
        />
        <p className="text-muted-foreground mt-1 text-xs">
          Optional. The paths tasks in this backlog may touch.
        </p>
      </div>
      <div>
        <label
          htmlFor="new-backlog-forbidden-scope"
          className="text-foreground block text-sm font-medium"
        >
          Forbidden scope
        </label>
        <Textarea
          id="new-backlog-forbidden-scope"
          name="forbiddenScope"
          value={forbiddenScope}
          onChange={(e) => setForbiddenScope(e.target.value)}
          rows={3}
          className="mt-1"
        />
        <p className="text-muted-foreground mt-1 text-xs">
          Optional. The paths tasks in this backlog may not touch.
        </p>
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Creating…" : "Create backlog"}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onCancel}
          disabled={pending}
        >
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** The one place that defines what "no filter" means for each URL-held
 *  filter (mirroring TaskListSection's FILTER_DEFAULTS, issue #150) — both
 *  the changeXFilter functions below and the "is any filter active" check
 *  that drives the Clear filters control read from here. */
const FILTER_DEFAULTS = {
  // "open" rather than "all", and the only default here that hides rows: a
  // shipped backlog is closed precisely so it stops filling this screen, so
  // the collection would defeat its own feature by showing closed ones by
  // default. "all" is one click away, and the URL says which is in force.
  status: "open",
  priority: "all",
  progress: "all",
  sort: "manual",
} as const;

/**
 * BacklogListSection is the Backlog collection view at
 * /projects/[projectId]/backlogs. List and Timeline are view modes of this one
 * screen (docs/ui-design.md rule 5), and backlog creation, editing and delete
 * all happen here rather than on a separate backlog-management screen —
 * actions live on the object they act on (rule 4).
 *
 * `priority`/`progress`/`sort` (issue #151) are the URL, applied server-side
 * by the caller (page.tsx) the same way TaskListSection's own filters are —
 * except `sort=dueOn`, which has no server-side meaning and is sorted here
 * instead, and the name search (`?q=`), which has no API support at all and
 * is read straight from `useSearchParams` and matched client-side, since
 * backlogs run orders of magnitude fewer per project than tasks.
 *
 * List mode also shows each row's own closed/total completion — the same
 * `backlogTaskCompletion` reading of `taskCount`/`closedTaskCount` the Board
 * mode's cards already use (issue #144) — and a trailing "Unclassified (n)"
 * row for tasks with no backlog at all (issue #152), matching the Task
 * collection's own Unclassified group. That row isn't a backlog: it has no
 * priority/progress of its own, so it drops out under either filter, and it
 * carries no Edit/Delete controls, just a link to the Task
 * collection filtered to `UNCLASSIFIED_BACKLOG`. It's List-only — Board's
 * axis is progress, which Unclassified tasks don't share one value of, and
 * Timeline is a dated bar per backlog, which Unclassified isn't one of — and
 * it disappears entirely at zero rather than reading "Unclassified (0)".
 */
export function BacklogListSection({
  projectId,
  backlogs,
  links = [],
  statusFilter = "open",
  priorityFilter,
  progressFilter,
  sort = "manual",
  unclassifiedCount = 0,
  initialView = "board",
  error = false,
}: {
  projectId: string;
  backlogs: Backlog[];
  /** The project's linked GitLab projects (issue #180), offered by the
   *  create/edit forms as a backlog's own destination for new issues. Empty
   *  — the default, and the case for a project with no GitLab connection —
   *  hides that field entirely. */
  links?: LinkedGitlabProject[];
  /** The applied `?status=`. Defaults to "open", which is also the API's own
   *  default — unlike the filters below, this one is never "all" unless it
   *  was asked for. */
  statusFilter?: StatusFilter;
  /** The applied `?priority=`; undefined means all of them. */
  priorityFilter?: Priority;
  /** The applied `?progress=`; undefined means all of them. */
  progressFilter?: Progress;
  /** The applied `?sort=`, or "manual" for the API's own default order. */
  sort?: BacklogSort;
  /** The project's task count with no backlog at all (issue #152), shown as
   *  a trailing "Unclassified" row in List mode — see the class doc comment.
   *  Defaults to 0, which keeps the row hidden for callers (tests, stories)
   *  that don't pass one. */
  unclassifiedCount?: number;
  /** The applied `?view=` (issue #153) — page.tsx already validated it, the
   *  same fallback-to-default treatment every other filter above gets.
   *  Board is the default: how far along each backlog is, is the first
   *  question asked of a backlog collection, and the board answers it
   *  without reading every row. */
  initialView?: ViewMode;
  /** Set when page.tsx's getBacklogs call failed (issue #155), mirroring
   *  TaskListSection's own `error` prop: the view toggle and filter row hide
   *  (there's nothing behind them to switch between or narrow), "New backlog"
   *  stays reachable, and the content area reports the failure instead of an
   *  empty list. */
  error?: boolean;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [view, setView] = useViewMode(initialView);

  // See the class doc comment: `?q=` narrows `backlogs` client-side rather than
  // going through the API, the same way TaskListSection's `?label=`/`?due=`
  // do.
  const search = (searchParams.get("q") ?? "").trim();

  // priority/progress are already applied server-side (the caller fetched
  // with them); dueOn is the one sort the API doesn't know, so it's applied
  // here, and the name search always is.
  const visibleBacklogs = useMemo(() => {
    let result = backlogs;
    if (search) {
      const q = search.toLowerCase();
      result = result.filter((b) => b.name.toLowerCase().includes(q));
    }
    if (sort === "dueOn") {
      result = [...result].sort(compareByDueOn);
    }
    return result;
  }, [backlogs, search, sort]);

  // Bulk editing, the same machinery the Task collection's List view uses
  // (issue #149): a backlog carries its own priority, progress and
  // open/closed status, so the four actions BulkActionBar owns are exactly
  // the ones that apply here — there is no fifth, since a backlog has no
  // parent to be moved into. List-only, like the Task collection's.
  const selection = useBulkSelection<BaseBulkAction>({
    visibleIds: visibleBacklogs.map((b) => b.id),
    noun: "backlog",
  });
  const { selected, setSelected } = selection;

  function patchSelected(action: BaseBulkAction, body: Record<string, string>) {
    return selection.run(action, (backlogId) =>
      fetch(`${API_PUBLIC_URL}/api/v1/backlogs/${backlogId}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify(body),
      }),
    );
  }

  function postSelected(action: "close" | "reopen") {
    return selection.run(action, (backlogId) =>
      fetch(`${API_PUBLIC_URL}/api/v1/backlogs/${backlogId}/${action}`, {
        method: "POST",
        credentials: "include",
        headers: csrfHeaders(),
      }),
    );
  }

  const hasActiveFilters =
    statusFilter !== FILTER_DEFAULTS.status ||
    priorityFilter !== undefined ||
    progressFilter !== undefined ||
    sort !== FILTER_DEFAULTS.sort ||
    search !== "";

  // See the class doc comment: the Unclassified row has no priority/progress
  // of its own, so an active filter on either always excludes it, same as a
  // backlog that doesn't match would be; the name search matches it against
  // its own label like any other row's name.
  const showUnclassified =
    unclassifiedCount > 0 &&
    !priorityFilter &&
    !progressFilter &&
    (search === "" || "unclassified".includes(search.toLowerCase()));

  /** Mirrors TaskListSection's updateQuery: every filter/sort choice belongs
   *  in the URL, so the screen stays shareable and reload-stable, and a
   *  value equal to that filter's default drops out of the query string
   *  rather than being spelled out. */
  function updateQuery(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(next)) {
      params.delete(key);
      if (value) params.set(key, value);
    }
    const query = params.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  function changeStatusFilter(value: StatusFilter) {
    updateQuery({
      status: value === FILTER_DEFAULTS.status ? undefined : value,
    });
  }

  function changePriorityFilter(value: "all" | Priority) {
    updateQuery({
      priority: value === FILTER_DEFAULTS.priority ? undefined : value,
    });
  }

  function changeProgressFilter(value: "all" | Progress) {
    updateQuery({
      progress: value === FILTER_DEFAULTS.progress ? undefined : value,
    });
  }

  function changeSort(value: BacklogSort) {
    updateQuery({ sort: value === FILTER_DEFAULTS.sort ? undefined : value });
  }

  function changeSearch(value: string) {
    updateQuery({ q: value.trim() === "" ? undefined : value });
  }

  function clearFilters() {
    router.push(pathname);
  }

  // Distinguishes *why* the list came back empty, the same way
  // TaskListSection's emptyFilterMessage does — a bare "no matches" doesn't
  // say whether it's the search term, the priority/progress filter, or (see
  // the render below) whether the project has no backlogs at all.
  function emptyFilterMessage(): string {
    if (search) {
      return `No backlogs match "${search}".`;
    }
    if (statusFilter === "closed") {
      return "No closed backlogs.";
    }
    if (priorityFilter) {
      return `No ${PRIORITY_LABELS[priorityFilter].toLowerCase()} priority backlogs.`;
    }
    if (progressFilter) {
      return `No ${PROGRESS_LABELS[progressFilter].toLowerCase()} backlogs.`;
    }
    return "No backlogs match the current filters.";
  }

  /** The empty state, shared by all three view modes.
   *
   *  It carries a way back to the closed backlogs because the filter row above
   *  is hidden while the list is empty — so a project whose only backlogs have
   *  all been closed would otherwise have no control anywhere on the screen
   *  that could reveal them again. */
  function emptyState() {
    if (hasActiveFilters) {
      return (
        <p className="text-muted-foreground text-sm">{emptyFilterMessage()}</p>
      );
    }
    return (
      <p className="text-muted-foreground text-sm">
        No backlogs yet.{" "}
        <button
          type="button"
          onClick={() => changeStatusFilter("all")}
          className="hover:text-foreground underline"
        >
          Show closed backlogs
        </button>
      </p>
    );
  }

  return (
    <Card>
      <CardHeader>
        {/* Two rows, same shape as the Task collection: the object's name and
            its object-level controls (view mode, create) on the top row, and
            the filter/sort controls left-aligned on their own row below. */}
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <CardTitle className="text-base font-medium">Backlogs</CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              {/* The view modes only make sense once backlogs exist (and
                  none of the current filters keep the project's own backlogs
                  hidden), but "New backlog" must stay reachable on an empty
                  project. Left off entirely on a load error, alongside the
                  filter row below, the same way TaskListSection's does. */}
              {!error &&
              (backlogs.length > 0 ||
                hasActiveFilters ||
                unclassifiedCount > 0) ? (
                <ViewModeToggle value={view} onChange={setView} />
              ) : null}
              {!creating ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setCreating(true)}
                >
                  <Plus className="size-4" aria-hidden />
                  New backlog
                </Button>
              ) : null}
            </div>
          </div>
          {/* Filters belong to the collection, not to one presentation of it
              (docs/ui-design.md rule 5), so they stay put across view modes
              and narrow the timeline the same way they narrow the list. Only
              shown once there's something to filter, same condition as the
              view toggle above. */}
          {!error &&
          (backlogs.length > 0 || hasActiveFilters || unclassifiedCount > 0) ? (
            <div className="flex flex-wrap items-center gap-2">
              <TaskSearchBox
                value={search}
                onChange={changeSearch}
                label="backlogs"
              />
              <Select
                value={statusFilter}
                onValueChange={(value) =>
                  changeStatusFilter(value as StatusFilter)
                }
              >
                <SelectTrigger size="sm" aria-label="Status" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="open">Open</SelectItem>
                  <SelectItem value="closed">Closed</SelectItem>
                  <SelectItem value="all">All statuses</SelectItem>
                </SelectContent>
              </Select>
              <Select
                value={priorityFilter ?? "all"}
                onValueChange={(value) =>
                  changePriorityFilter(value as "all" | Priority)
                }
              >
                <SelectTrigger size="sm" aria-label="Priority" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All priorities</SelectItem>
                  {PRIORITY_OPTIONS.map((option) => (
                    <SelectItem key={option.priority} value={option.priority}>
                      <PriorityDot priority={option.priority} />
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={progressFilter ?? "all"}
                onValueChange={(value) =>
                  changeProgressFilter(value as "all" | Progress)
                }
              >
                <SelectTrigger size="sm" aria-label="Progress" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All progress</SelectItem>
                  {PROGRESS_COLUMNS.map((option) => (
                    <SelectItem key={option.progress} value={option.progress}>
                      <ProgressDot progress={option.progress} />
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={sort}
                onValueChange={(value) => changeSort(value as BacklogSort)}
              >
                <SelectTrigger size="sm" aria-label="Sort" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="manual">Default order</SelectItem>
                  <SelectItem value="dueOn">Due date</SelectItem>
                  <SelectItem value="priority">Priority</SelectItem>
                  <SelectItem value="progress">Progress</SelectItem>
                </SelectContent>
              </Select>
              {/* Only appears once a filter actually differs from
                  FILTER_DEFAULTS, mirroring TaskListSection's Clear filters
                  control (issue #150). */}
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
            <NewBacklogForm
              projectId={projectId}
              links={links}
              onCancel={() => setCreating(false)}
            />
          </CreateFormRegion>
        ) : null}
        {error ? (
          <p className="text-destructive text-sm">
            Failed to load backlogs. Try refreshing the page.
          </p>
        ) : view === "board" ? (
          visibleBacklogs.length === 0 ? (
            emptyState()
          ) : (
            <BacklogBoardSection
              projectId={projectId}
              backlogs={visibleBacklogs}
            />
          )
        ) : view === "timeline" ? (
          visibleBacklogs.length === 0 ? (
            emptyState()
          ) : (
            <BacklogTimelineSection
              projectId={projectId}
              backlogs={visibleBacklogs}
            />
          )
        ) : (
          <div className="space-y-2">
            {/* Unlike Board/Timeline above, this branch's "empty" state also
                depends on the Unclassified row (see the class doc comment) —
                a project with no backlogs but some unclassified tasks still
                has something to show in List. */}
            {visibleBacklogs.length === 0 && !showUnclassified ? (
              emptyState()
            ) : (
              <>
                {/* The Unclassified row is deliberately outside this: it
                    isn't a backlog, so there is nothing to bulk-edit about
                    it. */}
                {visibleBacklogs.length > 0 ? (
                  <div className="mb-4 space-y-4">
                    <div className="flex items-center gap-2">
                      <SelectAllCheckbox
                        label="Select all backlogs"
                        ids={visibleBacklogs.map((b) => b.id)}
                        selected={selected}
                        onChange={setSelected}
                      />
                      <span className="text-muted-foreground text-xs">
                        Select all
                      </span>
                    </div>
                    <BulkActionBar
                      selection={selection}
                      onPriority={(priority) =>
                        patchSelected("priority", { priority })
                      }
                      onProgress={(progress) =>
                        patchSelected("progress", { progress })
                      }
                      onClose={() => postSelected("close")}
                      onReopen={() => postSelected("reopen")}
                    />
                  </div>
                ) : null}
                <ul className="space-y-2">
                  {visibleBacklogs.map((backlog) => {
                    // Same closed/total reading the Board mode's cards use (issue
                    // #144) — see the class doc comment.
                    const completion = backlogTaskCompletion(backlog);
                    return (
                      <li
                        key={backlog.id}
                        className="border-border rounded-md border px-3 py-2"
                      >
                        {editingId === backlog.id ? (
                          <BacklogEditForm
                            backlog={backlog}
                            links={links}
                            onSaved={() => {
                              // The row is rendered from the server-fetched
                              // list, so the saved values only appear after a
                              // refresh.
                              router.refresh();
                              setEditingId(null);
                            }}
                            onCancel={() => setEditingId(null)}
                          />
                        ) : (
                          <div className="flex items-center justify-between gap-4">
                            <RowCheckbox
                              label={`Select ${backlog.name}`}
                              id={backlog.id}
                              selected={selected}
                              onToggle={selection.toggle}
                            />
                            <div className="min-w-0 flex-1">
                              <div className="flex flex-wrap items-center gap-2">
                                {/* The name clips rather than wraps: a long one
                                  used to push the badges onto their own line
                                  and grow the row. The full text is on hover
                                  when it did clip. */}
                                <TruncatedName
                                  href={backlogPath(projectId, backlog.id)}
                                  text={backlog.name}
                                  className="text-foreground text-sm hover:underline"
                                />
                                <span className="flex shrink-0 items-center gap-2">
                                  {/* Only ever rendered when ?status= asked for
                                    closed backlogs — see ClosedBadge. */}
                                  <ClosedBadge status={backlog.status} />
                                  <PriorityBadge priority={backlog.priority} />
                                  <ProgressBadge progress={backlog.progress} />
                                </span>
                              </div>
                              {backlogScheduleLabel(backlog) ? (
                                <p className="text-muted-foreground truncate text-xs">
                                  {backlogScheduleLabel(backlog)}
                                </p>
                              ) : null}
                              {/* The fill is a second reading of the ratio stated
                          beside it, never the only one — same rule the
                          Board mode's cards and the timeline's bars
                          follow. */}
                              <div className="mt-1 flex items-center gap-2">
                                <div className="bg-muted h-1 w-24 shrink-0 overflow-hidden rounded-full">
                                  <div
                                    aria-hidden
                                    className="bg-primary h-full"
                                    style={{
                                      width: `${Math.round(completion.ratio * 100)}%`,
                                    }}
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
                              {/* Tasks live in the Task collection, filtered — this row
                          hands off to it instead of the list growing a second
                          place to browse tasks (docs/ui-design.md rule 5). */}
                              <Link
                                href={tasksPath(projectId, {
                                  backlogId: backlog.id,
                                })}
                                className="text-muted-foreground hover:text-foreground text-sm hover:underline"
                              >
                                View tasks
                              </Link>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => setEditingId(backlog.id)}
                              >
                                Edit
                              </Button>
                              <BacklogDeleteButton
                                backlog={backlog}
                                onDeleted={() => router.refresh()}
                              />
                            </div>
                          </div>
                        )}
                      </li>
                    );
                  })}
                  {showUnclassified ? (
                    <li className="border-border rounded-md border border-dashed px-3 py-2">
                      <Link
                        href={tasksPath(projectId, {
                          backlogId: UNCLASSIFIED_BACKLOG,
                        })}
                        className="text-foreground text-sm hover:underline"
                      >
                        Unclassified{" "}
                        <span className="text-muted-foreground text-xs">
                          ({unclassifiedCount})
                        </span>
                      </Link>
                    </li>
                  ) : null}
                </ul>
              </>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
