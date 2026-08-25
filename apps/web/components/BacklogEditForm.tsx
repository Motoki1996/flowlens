"use client";

import { useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { fromApiDate, toApiDate } from "@/lib/dates";
import type {
  ApiError,
  Backlog,
  LinkedGitlabProject,
  Priority,
  Progress,
} from "@/types";
import { PROGRESS_COLUMNS } from "@/lib/progress";
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

const PROJECT_DEFAULT_LINK = "project-default";

/**
 * LinkedGitlabProjectField picks the GitLab project this backlog's new tasks
 * get their issue created in (issue #180). Choosing "Project default" sends
 * null, which falls the backlog back to the project's own default link.
 *
 * It renders nothing when the project has no linked GitLab project at all:
 * there is no destination to choose between, and the field would only raise a
 * question the screen can't answer.
 */
export function LinkedGitlabProjectField({
  id,
  links,
  value,
  onChange,
}: {
  id: string;
  links: LinkedGitlabProject[];
  value: string | null;
  onChange: (value: string | null) => void;
}) {
  if (links.length === 0) return null;
  const projectDefault = links.find((l) => l.isDefault);

  return (
    <div>
      <label htmlFor={id} className="text-foreground block text-sm font-medium">
        GitLab project for new issues
      </label>
      <Select
        value={value ?? PROJECT_DEFAULT_LINK}
        onValueChange={(next) =>
          onChange(next === PROJECT_DEFAULT_LINK ? null : next)
        }
      >
        <SelectTrigger id={id} className="mt-1 w-full sm:w-80">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={PROJECT_DEFAULT_LINK}>
            {projectDefault
              ? `Project default (${projectDefault.pathWithNamespace})`
              : "Project default"}
          </SelectItem>
          {links.map((link) => (
            <SelectItem key={link.id} value={link.id}>
              {link.pathWithNamespace}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

/**
 * BacklogEditForm edits one backlog's attributes in place. It covers the
 * schedule as well as the name, so the action is "Edit", not "Rename".
 *
 * It is shared by the two screens that own the Backlog object: the collection
 * view's list row (BacklogListSection) and the single view (BacklogDetail) —
 * there is deliberately no /backlogs/[id]/edit route, since editing is an
 * action on the object and lives on the object's own screens
 * (docs/ui-design.md rule 4).
 *
 * `name` and `description` are not optional on the API's side, so they are
 * always sent.
 */
export function BacklogEditForm({
  backlog,
  links,
  onSaved,
  onCancel,
}: {
  backlog: Backlog;
  links: LinkedGitlabProject[];
  onSaved: (backlog: Backlog) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(backlog.name);
  const [description, setDescription] = useState(backlog.description);
  const [startDate, setStartDate] = useState(fromApiDate(backlog.startDate));
  const [dueOn, setDueOn] = useState(fromApiDate(backlog.dueOn));
  const [priority, setPriority] = useState<Priority>(backlog.priority);
  const [progress, setProgress] = useState<Progress>(backlog.progress);
  const [linkId, setLinkId] = useState<string | null>(
    backlog.defaultLinkedGitlabProjectId,
  );
  const [baseBranch, setBaseBranch] = useState(backlog.baseBranch);
  const [allowedScope, setAllowedScope] = useState(backlog.allowedScope);
  const [forbiddenScope, setForbiddenScope] = useState(backlog.forbiddenScope);
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
        `${API_PUBLIC_URL}/api/v1/backlogs/${backlog.id}`,
        {
          method: "PATCH",
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
        setError(body?.error.message ?? "Failed to update backlog.");
        return;
      }
      onSaved((await res.json()) as Backlog);
    } finally {
      setPending(false);
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-3"
      aria-label={`Edit ${backlog.name}`}
    >
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label
          htmlFor={`edit-backlog-name-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Name
        </label>
        <Input
          id={`edit-backlog-name-${backlog.id}`}
          name="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label
          htmlFor={`edit-backlog-description-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Description
        </label>
        <Textarea
          id={`edit-backlog-description-${backlog.id}`}
          name="description"
          aria-describedby={`edit-backlog-description-${backlog.id}-hint`}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mt-1 min-h-[150px]"
        />
        <p
          id={`edit-backlog-description-${backlog.id}-hint`}
          className="text-muted-foreground mt-1 text-xs"
        >
          Markdown supported — pasted URLs become links.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <DateField
          id={`edit-backlog-start-date-${backlog.id}`}
          label="Start date"
          value={startDate}
          onChange={setStartDate}
        />
        <DateField
          id={`edit-backlog-due-on-${backlog.id}`}
          label="Due date"
          value={dueOn}
          onChange={setDueOn}
        />
      </div>
      <div>
        <label
          htmlFor={`edit-backlog-priority-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Priority
        </label>
        <Select
          value={priority}
          onValueChange={(value) => setPriority(value as Priority)}
        >
          <SelectTrigger
            id={`edit-backlog-priority-${backlog.id}`}
            className="mt-1 w-full sm:w-40"
          >
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
        <label
          htmlFor={`edit-backlog-progress-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Progress
        </label>
        <Select
          value={progress}
          onValueChange={(value) => setProgress(value as Progress)}
        >
          <SelectTrigger
            id={`edit-backlog-progress-${backlog.id}`}
            className="mt-1 w-full sm:w-40"
          >
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
      <LinkedGitlabProjectField
        id={`edit-backlog-linked-gitlab-project-${backlog.id}`}
        links={links}
        value={linkId}
        onChange={setLinkId}
      />
      <div>
        <label
          htmlFor={`edit-backlog-base-branch-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Base branch
        </label>
        <Input
          id={`edit-backlog-base-branch-${backlog.id}`}
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
          htmlFor={`edit-backlog-allowed-scope-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Allowed scope
        </label>
        <Textarea
          id={`edit-backlog-allowed-scope-${backlog.id}`}
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
          htmlFor={`edit-backlog-forbidden-scope-${backlog.id}`}
          className="text-foreground block text-sm font-medium"
        >
          Forbidden scope
        </label>
        <Textarea
          id={`edit-backlog-forbidden-scope-${backlog.id}`}
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
          {pending ? "Saving…" : "Save"}
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
