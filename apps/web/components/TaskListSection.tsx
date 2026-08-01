"use client";

import { useMemo, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import dynamic from "next/dynamic";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { taskPath, UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { formatDate, toApiDate } from "@/lib/dates";
import type { ApiError, Backlog, Task, TaskDependency, TaskStatus } from "@/types";
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
import { SyncBadge } from "@/components/SyncBadge";

/**
 * The Timeline view mode pulls in the charting library, which the default List
 * mode has no use for — loading it on demand keeps that cost off the task
 * collection until someone actually switches views.
 */
const TaskTimelineSection = dynamic(
  () => import("@/components/TaskTimelineSection").then((m) => m.TaskTimelineSection),
  { loading: () => <p className="text-muted-foreground text-sm">Loading timeline…</p> },
);

type ViewMode = "list" | "timeline";

const UNCLASSIFIED = UNCLASSIFIED_BACKLOG;
const UNCLASSIFIED_LABEL = "Unclassified";

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
 * API fills the assignee in itself, and neither field is editable on the task
 * single view yet.
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
  error = false,
}: {
  projectId: string;
  tasks: Task[];
  backlogs: Backlog[];
  dependencies?: TaskDependency[];
  /** The `?backlog=` the screen was opened with, if any — how the backlog
   *  screens hand off to this collection. */
  initialBacklogFilter?: string;
  error?: boolean;
}) {
  const router = useRouter();
  const [view, setView] = useState<ViewMode>("list");
  const [creating, setCreating] = useState(false);
  const [statusFilter, setStatusFilter] = useState<"all" | TaskStatus>("all");
  const [backlogFilter, setBacklogFilter] = useState<"all" | string>(
    initialBacklogFilter ?? "all",
  );
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [targetBacklogId, setTargetBacklogId] = useState("");
  const [assigning, setAssigning] = useState(false);
  const [assignError, setAssignError] = useState<string | null>(null);

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

  // Bulk assign moves tasks *into* a backlog, so Unclassified is not a destination.
  const assignOptions = useMemo(
    () => backlogs.map((b) => ({ value: b.id, label: b.name })),
    [backlogs],
  );

  const filtered = useMemo(() => {
    return tasks.filter((t) => {
      if (statusFilter !== "all" && t.status !== statusFilter) return false;
      if (backlogFilter !== "all") {
        const key = t.backlogId ?? UNCLASSIFIED;
        if (key !== backlogFilter) return false;
      }
      return true;
    });
  }, [tasks, statusFilter, backlogFilter]);

  const groups = useMemo(() => {
    const byBacklog = new Map<string, Task[]>();
    for (const t of filtered) {
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
  }, [filtered, backlogs]);

  /**
   * Filtering by backlog is how the backlog screens hand off to this one, so
   * the choice belongs in the URL: the screen stays shareable and the browser's
   * back button walks the filter. history.replaceState keeps that a client-side
   * edit — router.replace would re-render the whole tree just to change a
   * filter the client already applied.
   */
  function changeBacklogFilter(value: string) {
    setBacklogFilter(value);
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (value === "all") {
      url.searchParams.delete("backlog");
    } else {
      url.searchParams.set("backlog", value);
    }
    window.history.replaceState(null, "", url);
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
      const responses = await Promise.all(
        Array.from(selected).map((taskId) =>
          fetch(`${API_PUBLIC_URL}/api/v1/tasks/${taskId}/assign-backlog`, {
            method: "POST",
            credentials: "include",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ backlogId: targetBacklogId }),
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

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Tasks</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            {/* The view modes and filters only make sense once tasks exist, but
                "New task" must stay reachable on an empty project. */}
            {!error && tasks.length > 0 ? (
              <>
                <div className="flex" role="group" aria-label="View">
                  <Button
                    type="button"
                    variant={view === "list" ? "default" : "outline"}
                    size="sm"
                    className="rounded-r-none"
                    onClick={() => setView("list")}
                  >
                    List
                  </Button>
                  <Button
                    type="button"
                    variant={view === "timeline" ? "default" : "outline"}
                    size="sm"
                    className="rounded-l-none"
                    onClick={() => setView("timeline")}
                  >
                    Timeline
                  </Button>
                </div>
                {/* Filters belong to the collection, not to one presentation of
                    it (docs/ui-design.md rule 5), so they stay put across view
                    modes and narrow the timeline the same way they narrow the
                    list. Keeping them mounted is also what holds this
                    right-aligned cluster still: unmounting them on every view
                    switch slid the buttons out from under the pointer that had
                    just clicked them.

                    Status is a short fixed list, so it stays a Select; backlogs
                    grow with the project and get the searchable Combobox. */}
                <Select
                  value={statusFilter}
                  onValueChange={(value) => setStatusFilter(value as "all" | TaskStatus)}
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
              </>
            ) : null}
            {!creating ? (
              <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
                New task
              </Button>
            ) : null}
          </div>
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
          <p className="text-muted-foreground text-sm">No tasks match the current filters.</p>
        ) : view === "timeline" ? (
          <TaskTimelineSection
            projectId={projectId}
            tasks={filtered}
            allTasks={tasks}
            dependencies={dependencies}
          />
        ) : (
          <div className="space-y-6">
            {groups.map((group) => {
              const selectable = group.key === UNCLASSIFIED && backlogs.length > 0;
              return (
                <div key={group.key}>
                  <h3 className="text-muted-foreground mb-2 text-sm font-medium">
                    {group.name} ({group.tasks.length})
                  </h3>
                  {selectable && selected.size > 0 ? (
                    <div className="mb-2 flex flex-wrap items-center gap-2">
                      {assignError ? <span className="text-destructive text-xs">{assignError}</span> : null}
                      <span className="text-muted-foreground text-xs">{selected.size} selected</span>
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
                      <Button size="sm" onClick={handleAssignSelected} disabled={!targetBacklogId || assigning}>
                        {assigning ? "Assigning…" : "Assign to backlog"}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setSelected(new Set())}
                        disabled={assigning}
                      >
                        Cancel
                      </Button>
                    </div>
                  ) : null}
                  <ul className="space-y-2">
                    {group.tasks.map((task) => (
                      <li key={task.id} className="flex items-center gap-2">
                        {selectable ? (
                          <input
                            type="checkbox"
                            aria-label={`Select ${task.title}`}
                            checked={selected.has(task.id)}
                            onChange={() => toggleSelected(task.id)}
                            className="border-input h-4 w-4 shrink-0 rounded"
                          />
                        ) : null}
                        <Link
                          href={taskPath(projectId, task.id)}
                          className="border-border hover:border-ring flex flex-1 items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
                        >
                          <span className="text-foreground">{task.title}</span>
                          <span className="text-muted-foreground flex shrink-0 items-center gap-3 text-xs">
                            {task.assigneeGitlabUsername ? (
                              <span>{task.assigneeGitlabUsername}</span>
                            ) : null}
                            {task.dueOn ? <span>Due {formatDate(task.dueOn)}</span> : null}
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
        )}
      </CardContent>
    </Card>
  );
}
