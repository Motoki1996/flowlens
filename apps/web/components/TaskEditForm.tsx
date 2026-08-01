"use client";

import { useMemo, useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { fromApiDate, toApiDate } from "@/lib/dates";
import type { ApiError, Backlog, Task } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { DateField } from "@/components/DateField";

const UNCLASSIFIED_LABEL = "未分類";

/** parseLabels reads the comma-separated label input back into the API's array. */
function parseLabels(raw: string): string[] {
  return raw
    .split(",")
    .map((label) => label.trim())
    .filter((label) => label.length > 0);
}

/**
 * TaskEditForm edits one task's attributes in place inside the task single
 * view. There is deliberately no /tasks/[id]/edit route: editing is an action
 * on the Task object, so it lives on the Task's own screen (docs/ui-design.md,
 * rule 4).
 *
 * It PATCHes only the fields it shows. The API treats an absent key as "leave
 * this alone", so position — which this form has no control for — survives an
 * edit instead of being reset to 0.
 */
export function TaskEditForm({
  task,
  backlogs,
  onSaved,
  onCancel,
}: {
  task: Task;
  backlogs: Backlog[];
  onSaved: (task: Task) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description);
  const [backlogId, setBacklogId] = useState(task.backlogId ?? UNCLASSIFIED_BACKLOG);
  const [assignee, setAssignee] = useState(task.assigneeGitlabUsername);
  const [labels, setLabels] = useState(task.labels.join(", "));
  const [startDate, setStartDate] = useState(fromApiDate(task.startDate));
  const [dueOn, setDueOn] = useState(fromApiDate(task.dueOn));
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const backlogOptions = useMemo(
    () => [
      { value: UNCLASSIFIED_BACKLOG, label: UNCLASSIFIED_LABEL },
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
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title,
          description,
          backlogId: backlogId === UNCLASSIFIED_BACKLOG ? null : backlogId,
          assigneeGitlabUsername: assignee,
          labels: parseLabels(labels),
          startDate: toApiDate(startDate),
          dueOn: toApiDate(dueOn),
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to save task.");
        return;
      }
      onSaved((await res.json()) as Task);
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="Edit task">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="edit-task-title" className="text-foreground block text-sm font-medium">
          Title
        </label>
        <Input
          id="edit-task-title"
          name="title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label htmlFor="edit-task-description" className="text-foreground block text-sm font-medium">
          Description
        </label>
        <Textarea
          id="edit-task-description"
          name="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label htmlFor="edit-task-assignee" className="text-foreground block text-sm font-medium">
            Assignee
          </label>
          <Input
            id="edit-task-assignee"
            name="assigneeGitlabUsername"
            value={assignee}
            onChange={(e) => setAssignee(e.target.value)}
            placeholder="GitLab username"
            className="mt-1"
          />
        </div>
        <div>
          <label htmlFor="edit-task-labels" className="text-foreground block text-sm font-medium">
            Labels
          </label>
          <Input
            id="edit-task-labels"
            name="labels"
            value={labels}
            onChange={(e) => setLabels(e.target.value)}
            placeholder="bug, urgent"
            className="mt-1"
          />
          <p className="text-muted-foreground mt-1 text-xs">Comma-separated.</p>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div>
          <label htmlFor="edit-task-backlog" className="text-foreground block text-sm font-medium">
            Backlog
          </label>
          <Combobox
            id="edit-task-backlog"
            options={backlogOptions}
            value={backlogId}
            onChange={setBacklogId}
            searchPlaceholder="Search backlogs…"
            emptyText="No backlog found."
            className="mt-1"
          />
        </div>
        <DateField
          id="edit-task-start-date"
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField id="edit-task-due-on" label="Due date" value={dueOn} onChange={setDueOn} />
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Saving…" : "Save"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
