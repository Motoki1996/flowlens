"use client";

import { useMemo, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { toApiDate } from "@/lib/dates";
import { UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { PRIORITY_COLUMNS } from "@/lib/priority";
import { PROGRESS_COLUMNS } from "@/lib/progress";
import { SIZE_OPTIONS, SIZE_POINTS } from "@/lib/size";
import type { ApiError, Backlog, Epic, Priority, Progress, Size } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
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

const UNCLASSIFIED = UNCLASSIFIED_BACKLOG;
const UNCLASSIFIED_LABEL = "Unclassified";

/** The Combobox value standing for "not in an epic" — the state every task
 *  in a project that doesn't use the rung is in. */
const NO_EPIC = "no-epic";

/**
 * NewTaskForm is the inline task-creation form. Assignee and labels are
 * deliberately absent: on a project with a linked GitLab project the API
 * fills the assignee in itself, and both fields are edited on the task single
 * view instead (issue #80), once the task exists.
 *
 * It lives in its own module because two screens create tasks: the Task
 * collection (TaskListSection) and a backlog's own single view, which creates
 * them into that backlog. EpicForm is the same arrangement for epics — and
 * the reason this one had to be lifted out of TaskListSection at all.
 */
export function NewTaskForm({
  projectId,
  backlogs,
  epics = [],
  defaultBacklogId = null,
  defaultEpicId = null,
  onCreated,
  onCancel,
}: {
  projectId: string;
  /** The backlogs offered as the new task's home. On the Backlog single view
   *  this is that one backlog, since the task is being created *into* it. */
  backlogs: Backlog[];
  /** The project's epics, offered as the new task's epic. Empty — a project
   *  that doesn't use the rung — drops the field entirely, the same rule the
   *  Task single view's own Epic control follows. */
  epics?: Epic[];
  /** Pre-selects the backlog, so creating a task from a backlog's own screen
   *  files it there without a second choice. null leaves it Unclassified,
   *  which is what the Task collection wants. */
  defaultBacklogId?: string | null;
  /** Pre-selects the epic, for a screen that already knows it. */
  defaultEpicId?: string | null;
  /** Called after a successful create, before onCancel closes the form. The
   *  Task collection refreshes the route it is already on; a caller
   *  elsewhere may need to do more. */
  onCreated?: () => void;
  onCancel: () => void;
}) {
  const router = useRouter();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [backlogId, setBacklogId] = useState(defaultBacklogId ?? UNCLASSIFIED);
  const [epicId, setEpicId] = useState(defaultEpicId ?? NO_EPIC);
  const [startDate, setStartDate] = useState<Date | undefined>(undefined);
  const [dueOn, setDueOn] = useState<Date | undefined>(undefined);
  const [priority, setPriority] = useState<Priority>("medium");
  // Sizing at creation time, not only in the edit form: a size nobody sets
  // leaves every task at the default, which makes points-based velocity a
  // flat multiple of the task count and the whole feature inert.
  const [size, setSize] = useState<Size>("m");
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

  // Narrowed to the chosen backlog's epics: naming an epic from another
  // backlog would move the task out of the backlog just picked, which is not
  // what a form with both fields on screen reads as. (The API resolves it the
  // same way — the epic wins — so this is the UI refusing to state a
  // contradiction, not a second rule.)
  const epicOptions = useMemo(
    () => [
      { value: NO_EPIC, label: "No epic" },
      ...epics
        .filter((e) => e.backlogId === (backlogId === UNCLASSIFIED ? null : backlogId))
        .map((e) => ({ value: e.id, label: e.name })),
    ],
    [epics, backlogId],
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
          epicId: epicId === NO_EPIC ? null : epicId,
          startDate: toApiDate(startDate),
          dueOn: toApiDate(dueOn),
          priority,
          size,
          progress,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to create task.");
        return;
      }
      router.refresh();
      onCreated?.();
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
          aria-describedby="new-task-description-hint"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1 min-h-[150px]"
        />
        <p id="new-task-description-hint" className="text-muted-foreground mt-1 text-xs">
          Markdown supported — pasted URLs become links.
        </p>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
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
        {epics.length > 0 ? (
          <div>
            <label htmlFor="new-task-epic" className="text-foreground block text-sm font-medium">
              Epic
            </label>
            <Combobox
              id="new-task-epic"
              options={epicOptions}
              value={epicOptions.some((o) => o.value === epicId) ? epicId : NO_EPIC}
              onChange={setEpicId}
              searchPlaceholder="Search epics…"
              emptyText="No epic found."
              className="mt-1"
            />
          </div>
        ) : null}
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
        <label htmlFor="new-task-size" className="text-foreground block text-sm font-medium">
          Size
        </label>
        <Select value={size} onValueChange={(value) => setSize(value as Size)}>
          <SelectTrigger id="new-task-size" className="mt-1 w-full sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {SIZE_OPTIONS.map((option) => (
              <SelectItem key={option.size} value={option.size}>
                {option.label} ({SIZE_POINTS[option.size]} pts)
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

