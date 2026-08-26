"use client";

import { useMemo, useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { UNCLASSIFIED_BACKLOG } from "@/lib/routes";
import { fromApiDate, toApiDate } from "@/lib/dates";
import type {
  GitlabLabelOption,
  GitlabMemberOption,
  ApiError,
  Backlog,
  Priority,
  Size,
  Progress,
  Task,
} from "@/types";
import { PROGRESS_COLUMNS } from "@/lib/progress";
import { SIZE_OPTIONS, SIZE_POINTS } from "@/lib/size";
import { PRIORITY_OPTIONS } from "@/lib/priority";
import { PriorityDot } from "@/components/PriorityBadge";
import { ProgressDot } from "@/components/ProgressBadge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { MultiCombobox } from "@/components/ui/multi-combobox";
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

const UNCLASSIFIED_LABEL = "Unclassified";

// UNASSIGNED is the assignee Combobox's sentinel for "no assignee", mirroring
// UNCLASSIFIED_BACKLOG's role for the backlog picker — a GitLab user ID is
// never negative, so it can't collide with a real option's value.
const UNASSIGNED = "unassigned";

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
 * this alone", so a field this form has no control for survives an edit
 * untouched.
 */
export function TaskEditForm({
  task,
  backlogs,
  assigneeOptions = null,
  labelOptions = null,
  onSaved,
  onCancel,
}: {
  task: Task;
  backlogs: Backlog[];
  // The task's linked GitLab project's members/labels, or null when the
  // project has no default linked GitLab project — assignee/labels fall
  // back to free-text entry in that case, since a GitLab user ID has no
  // local equivalent to pick from (issue #80).
  assigneeOptions?: GitlabMemberOption[] | null;
  labelOptions?: GitlabLabelOption[] | null;
  onSaved: (task: Task) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description);
  const [backlogId, setBacklogId] = useState(task.backlogId ?? UNCLASSIFIED_BACKLOG);
  const [assignee, setAssignee] = useState(task.assigneeGitlabUsername);
  const [assigneeUserId, setAssigneeUserId] = useState(
    task.assigneeGitlabUserId != null ? String(task.assigneeGitlabUserId) : UNASSIGNED,
  );
  const [labels, setLabels] = useState(task.labels.join(", "));
  const [labelsList, setLabelsList] = useState<string[]>(task.labels);
  const [startDate, setStartDate] = useState(fromApiDate(task.startDate));
  const [dueOn, setDueOn] = useState(fromApiDate(task.dueOn));
  const [priority, setPriority] = useState<Priority>(task.priority);
  const [size, setSize] = useState<Size>(task.size);
  const [progress, setProgress] = useState<Progress>(task.progress);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const backlogOptions = useMemo(
    () => [
      { value: UNCLASSIFIED_BACKLOG, label: UNCLASSIFIED_LABEL },
      ...backlogs.map((b) => ({ value: b.id, label: b.name })),
    ],
    [backlogs],
  );

  // The current assignee is kept as an option even if it fell off the
  // fetched member list (e.g. a member removed from the GitLab project),
  // so opening the form never silently drops the existing selection.
  const assigneeSelectOptions = useMemo(() => {
    if (!assigneeOptions) return [];
    const known = assigneeOptions.map((m) => ({
      value: String(m.id),
      label: m.name ? `${m.name} (@${m.username})` : m.username,
    }));
    if (task.assigneeGitlabUserId != null && !assigneeOptions.some((m) => m.id === task.assigneeGitlabUserId)) {
      known.unshift({
        value: String(task.assigneeGitlabUserId),
        label: task.assigneeGitlabUsername || `User #${task.assigneeGitlabUserId}`,
      });
    }
    return [{ value: UNASSIGNED, label: "Unassigned" }, ...known];
  }, [assigneeOptions, task.assigneeGitlabUserId, task.assigneeGitlabUsername]);

  // Same idea for labels: a label already on the task stays selectable even
  // if it isn't (or is no longer) one of the GitLab project's labels.
  const labelSelectOptions = useMemo(() => {
    if (!labelOptions) return [];
    const known = labelOptions.map((l) => ({ value: l.name, label: l.name }));
    const knownNames = new Set(known.map((o) => o.value));
    for (const label of task.labels) {
      if (!knownNames.has(label)) {
        known.push({ value: label, label });
        knownNames.add(label);
      }
    }
    return known;
  }, [labelOptions, task.labels]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!title.trim()) {
      setError("Task title is required.");
      return;
    }

    const assigneeFields = assigneeOptions
      ? {
          assigneeGitlabUserId: assigneeUserId === UNASSIGNED ? null : Number(assigneeUserId),
          assigneeGitlabUsername:
            assigneeUserId === UNASSIGNED
              ? ""
              : (assigneeOptions.find((m) => String(m.id) === assigneeUserId)?.username ?? ""),
        }
      : { assigneeGitlabUsername: assignee };

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          title,
          description,
          backlogId: backlogId === UNCLASSIFIED_BACKLOG ? null : backlogId,
          ...assigneeFields,
          labels: labelOptions ? labelsList : parseLabels(labels),
          startDate: toApiDate(startDate),
          dueOn: toApiDate(dueOn),
          priority,
          size,
          progress,
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
          aria-describedby="edit-task-description-hint"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1 min-h-[150px]"
        />
        <p id="edit-task-description-hint" className="text-muted-foreground mt-1 text-xs">
          Markdown supported — pasted URLs become links.
        </p>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label htmlFor="edit-task-assignee" className="text-foreground block text-sm font-medium">
            Assignee
          </label>
          {assigneeOptions ? (
            <Combobox
              id="edit-task-assignee"
              options={assigneeSelectOptions}
              value={assigneeUserId}
              onChange={setAssigneeUserId}
              searchPlaceholder="Search members…"
              emptyText="No member found."
              className="mt-1"
            />
          ) : (
            <Input
              id="edit-task-assignee"
              name="assigneeGitlabUsername"
              value={assignee}
              onChange={(e) => setAssignee(e.target.value)}
              placeholder="GitLab username"
              className="mt-1"
            />
          )}
        </div>
        <div>
          <label htmlFor="edit-task-labels" className="text-foreground block text-sm font-medium">
            Labels
          </label>
          {labelOptions ? (
            <MultiCombobox
              id="edit-task-labels"
              options={labelSelectOptions}
              value={labelsList}
              onChange={setLabelsList}
              searchPlaceholder="Search or add a label…"
              emptyText="No label found."
              className="mt-1"
            />
          ) : (
            <>
              <Input
                id="edit-task-labels"
                name="labels"
                value={labels}
                onChange={(e) => setLabels(e.target.value)}
                placeholder="bug, urgent"
                className="mt-1"
              />
              <p className="text-muted-foreground mt-1 text-xs">Comma-separated.</p>
            </>
          )}
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
      <div>
        <label htmlFor="edit-task-priority" className="text-foreground block text-sm font-medium">
          Priority
        </label>
        <Select value={priority} onValueChange={(value) => setPriority(value as Priority)}>
          <SelectTrigger id="edit-task-priority" className="mt-1 w-full sm:w-40">
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
        <label htmlFor="edit-task-size" className="text-foreground block text-sm font-medium">
          Size
        </label>
        <Select value={size} onValueChange={(value) => setSize(value as Size)}>
          <SelectTrigger id="edit-task-size" className="mt-1 w-full sm:w-40">
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
        <p className="text-muted-foreground mt-1 text-xs">
          Weights this task in the project&apos;s velocity. Not synced to GitLab.
        </p>
      </div>
      <div>
        <label htmlFor="edit-task-progress" className="text-foreground block text-sm font-medium">
          Progress
        </label>
        <Select value={progress} onValueChange={(value) => setProgress(value as Progress)}>
          <SelectTrigger id="edit-task-progress" className="mt-1 w-full sm:w-40">
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
