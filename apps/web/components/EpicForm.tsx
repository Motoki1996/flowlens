"use client";

import { useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { fromApiDate, toApiDate } from "@/lib/dates";
import type {
  ApiError,
  Backlog,
  Epic,
  LinkedGitlabProject,
  Priority,
  Progress,
  Task,
} from "@/types";
import { PROGRESS_COLUMNS } from "@/lib/progress";
import { PRIORITY_COLUMNS } from "@/lib/priority";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
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
import { LinkedGitlabProjectField } from "@/components/BacklogEditForm";
import { EpicTaskPicker, epicTasksBody } from "@/components/EpicTaskPicker";

/** The Select value standing for "no backlog" — Radix Select has no
 *  empty-string item, and the API's own spelling for it is `null`, which a
 *  Select can't hold either. The same trick LinkedGitlabProjectField uses. */
const NO_BACKLOG = "no-backlog";

/**
 * EpicForm creates or edits one epic. Unlike the Backlog collection, which
 * has a separate create form and edit form, an epic has one: the two differ
 * only in which request they send, and a second copy would be one more place
 * for a field to go missing.
 *
 * `epic` absent means create (POST to the project's epic collection); present
 * means edit that epic in place (PATCH). Editing has no route of its own —
 * it's an action on the object and lives on the object's own screens
 * (docs/ui-design.md rule 4).
 *
 * `name` and `description` are always sent on an edit: they are not optional
 * on the API's side.
 */
export function EpicForm({
  projectId,
  epic,
  backlogs,
  links,
  tasks = [],
  defaultBacklogId = null,
  onSaved,
  onCancel,
}: {
  projectId: string;
  /** The epic being edited; omitted creates a new one. */
  epic?: Epic;
  /** The project's backlogs, offered as the epic's parent. */
  backlogs: Backlog[];
  /** The project's tasks, for the "tasks in this epic" picker. Empty drops
   *  the field — a caller with no task list to offer (a screen that hasn't
   *  fetched one) shouldn't imply the epic has no tasks. */
  tasks?: Task[];
  /** The project's linked GitLab projects, offered as this epic's own
   *  destination for new issues. Empty — the case for a project with no
   *  GitLab connection — hides that field entirely. */
  links: LinkedGitlabProject[];
  /** Pre-selects the backlog on a create form, so "New epic" from a backlog's
   *  own screen files it there without a second choice. */
  defaultBacklogId?: string | null;
  onSaved: (epic: Epic) => void;
  onCancel: () => void;
}) {
  const editing = epic !== undefined;
  const idPrefix = editing ? `edit-epic-${epic.id}` : "new-epic";

  const [name, setName] = useState(epic?.name ?? "");
  const [description, setDescription] = useState(epic?.description ?? "");
  const [backlogId, setBacklogId] = useState<string | null>(
    epic ? epic.backlogId : defaultBacklogId,
  );
  const [startDate, setStartDate] = useState(fromApiDate(epic?.startDate ?? null));
  const [dueOn, setDueOn] = useState(fromApiDate(epic?.dueOn ?? null));
  const [priority, setPriority] = useState<Priority>(epic?.priority ?? "medium");
  const [progress, setProgress] = useState<Progress>(epic?.progress ?? "not_started");
  const [linkId, setLinkId] = useState<string | null>(
    epic?.defaultLinkedGitlabProjectId ?? null,
  );
  const [baseBranch, setBaseBranch] = useState(epic?.baseBranch ?? "");
  const [allowedScope, setAllowedScope] = useState(epic?.allowedScope ?? "");
  const [forbiddenScope, setForbiddenScope] = useState(epic?.forbiddenScope ?? "");
  // The task set is edited alongside the epic's own fields but written by its
  // own endpoint, so it starts from whichever tasks already point at this
  // epic rather than from a field on the epic itself.
  const [taskIds, setTaskIds] = useState<string[]>(() =>
    epic ? tasks.filter((t) => t.epicId === epic.id).map((t) => t.id) : [],
  );
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  // Offered only when the caller actually handed over a task list; a screen
  // that didn't fetch one has nothing to show and must not imply the epic is
  // empty.
  const showTaskPicker = tasks.length > 0;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setError("Epic name is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(
        editing
          ? `${API_PUBLIC_URL}/api/v1/epics/${epic.id}`
          : `${API_PUBLIC_URL}/api/v1/projects/${projectId}/epics`,
        {
          method: editing ? "PATCH" : "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({
            name,
            description,
            backlogId,
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
        setError(
          body?.error.message ??
            (editing ? "Failed to update epic." : "Failed to create epic."),
        );
        return;
      }
      const saved = (await res.json()) as Epic;

      // The task set is a second request: it is a relationship between two
      // objects, not a column on this one. It runs only when the picker is
      // actually on screen and has something to say — otherwise a form that
      // never showed it would empty the epic on every save.
      if (showTaskPicker && (taskIds.length > 0 || editing)) {
        const tasksRes = await fetch(`${API_PUBLIC_URL}/api/v1/epics/${saved.id}/tasks`, {
          method: "PATCH",
          credentials: "include",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: epicTasksBody(taskIds),
        });
        if (!tasksRes.ok) {
          const body = (await tasksRes.json().catch(() => null)) as ApiError | null;
          // The epic itself saved; only its tasks didn't. Saying so is the
          // difference between "try again" and "you now have a duplicate".
          setError(
            body?.error.message ??
              "The epic was saved, but its tasks could not be updated. Try again from the epic.",
          );
          return;
        }
      }

      onSaved(saved);
    } finally {
      setPending(false);
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-3"
      aria-label={editing ? `Edit ${epic.name}` : "New epic"}
    >
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <div>
        <label
          htmlFor={`${idPrefix}-name`}
          className="text-foreground block text-sm font-medium"
        >
          Name
        </label>
        <Input
          id={`${idPrefix}-name`}
          name="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>

      <div>
        <label
          htmlFor={`${idPrefix}-description`}
          className="text-foreground block text-sm font-medium"
        >
          Description
        </label>
        <Textarea
          id={`${idPrefix}-description`}
          name="description"
          aria-describedby={`${idPrefix}-description-hint`}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1 min-h-[150px]"
        />
        <p
          id={`${idPrefix}-description-hint`}
          className="text-muted-foreground mt-1 text-xs"
        >
          Markdown supported — pasted URLs become links.
        </p>
      </div>

      <div>
        <label
          htmlFor={`${idPrefix}-backlog`}
          className="text-foreground block text-sm font-medium"
        >
          Backlog
        </label>
        <Select
          value={backlogId ?? NO_BACKLOG}
          onValueChange={(next) => setBacklogId(next === NO_BACKLOG ? null : next)}
        >
          <SelectTrigger id={`${idPrefix}-backlog`} className="mt-1 w-full sm:w-80">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NO_BACKLOG}>No backlog</SelectItem>
            {backlogs.map((backlog) => (
              <SelectItem key={backlog.id} value={backlog.id}>
                {backlog.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {editing ? (
          <p className="text-muted-foreground mt-1 text-xs">
            Moving this epic to another backlog moves its tasks with it.
          </p>
        ) : null}
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <DateField
          id={`${idPrefix}-start-date`}
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField
          id={`${idPrefix}-due-on`}
          label="Due date"
          value={dueOn}
          onChange={setDueOn}
        />
      </div>

      <div>
        <label
          htmlFor={`${idPrefix}-priority`}
          className="text-foreground block text-sm font-medium"
        >
          Priority
        </label>
        <Select value={priority} onValueChange={(value) => setPriority(value as Priority)}>
          <SelectTrigger id={`${idPrefix}-priority`} className="mt-1 w-full sm:w-40">
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
        <label
          htmlFor={`${idPrefix}-progress`}
          className="text-foreground block text-sm font-medium"
        >
          Progress
        </label>
        <Select value={progress} onValueChange={(value) => setProgress(value as Progress)}>
          <SelectTrigger id={`${idPrefix}-progress`} className="mt-1 w-full sm:w-40">
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

      {showTaskPicker ? (
        <div>
          <label
            htmlFor={`${idPrefix}-tasks`}
            className="text-foreground block text-sm font-medium"
          >
            Tasks in this epic
          </label>
          <div className="mt-1">
            <EpicTaskPicker
              id={`${idPrefix}-tasks`}
              tasks={tasks}
              backlogId={backlogId}
              epicId={epic?.id}
              value={taskIds}
              onChange={setTaskIds}
              disabled={pending}
            />
          </div>
          <p className="text-muted-foreground mt-1 text-xs">
            Optional. A task filed here moves to this epic&apos;s backlog with it;
            unticking one leaves it in the backlog, outside any epic.
          </p>
        </div>
      ) : null}

      <LinkedGitlabProjectField
        id={`${idPrefix}-linked-gitlab-project`}
        links={links}
        value={linkId}
        onChange={setLinkId}
      />

      <div>
        <label
          htmlFor={`${idPrefix}-base-branch`}
          className="text-foreground block text-sm font-medium"
        >
          Base branch
        </label>
        <Input
          id={`${idPrefix}-base-branch`}
          name="baseBranch"
          value={baseBranch}
          onChange={(e) => setBaseBranch(e.target.value)}
          placeholder="main"
          className="mt-1 sm:w-80"
        />
        <p className="text-muted-foreground mt-1 text-xs">
          Optional. The branch tasks in this epic are meant to branch from. Left
          empty, the backlog&apos;s own base branch applies.
        </p>
      </div>

      <div>
        <label
          htmlFor={`${idPrefix}-allowed-scope`}
          className="text-foreground block text-sm font-medium"
        >
          Allowed scope
        </label>
        <Textarea
          id={`${idPrefix}-allowed-scope`}
          name="allowedScope"
          value={allowedScope}
          onChange={(e) => setAllowedScope(e.target.value)}
          rows={3}
          className="mt-1"
        />
        <p className="text-muted-foreground mt-1 text-xs">
          Optional. The paths tasks in this epic may touch. Left empty, the
          backlog&apos;s own applies.
        </p>
      </div>

      <div>
        <label
          htmlFor={`${idPrefix}-forbidden-scope`}
          className="text-foreground block text-sm font-medium"
        >
          Forbidden scope
        </label>
        <Textarea
          id={`${idPrefix}-forbidden-scope`}
          name="forbiddenScope"
          value={forbiddenScope}
          onChange={(e) => setForbiddenScope(e.target.value)}
          rows={3}
          className="mt-1"
        />
        <p className="text-muted-foreground mt-1 text-xs">
          Optional. The paths tasks in this epic may not touch. Left empty, the
          backlog&apos;s own applies.
        </p>
      </div>

      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending
            ? editing
              ? "Saving…"
              : "Creating…"
            : editing
              ? "Save"
              : "Create epic"}
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
