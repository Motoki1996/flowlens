"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { tasksPath } from "@/lib/routes";
import type {
  ApiError,
  Backlog,
  GitlabLabelOption,
  GitlabMemberOption,
  Task,
  TaskDependency,
} from "@/types";
import { formatDate } from "@/lib/dates";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { AIContextSection } from "@/components/AIContextSection";
import { TaskDependencySection } from "@/components/TaskDependencySection";
import { TaskEditForm } from "@/components/TaskEditForm";
import { PriorityBadge } from "@/components/PriorityBadge";
import { ProgressBadge } from "@/components/ProgressBadge";
import { SyncBadge } from "@/components/SyncBadge";

/** CloseReopenButton toggles a task between open and closed in place. */
function CloseReopenButton({
  task,
  onChanged,
}: {
  task: Task;
  onChanged: (task: Task) => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const action = task.status === "open" ? "close" : "reopen";

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}/${action}`, {
        method: "POST",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? `Failed to ${action} task.`);
        return;
      }
      onChanged((await res.json()) as Task);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col items-end gap-1">
      {error ? <span className="text-destructive text-xs">{error}</span> : null}
      <Button variant="outline" size="sm" onClick={handleClick} disabled={pending}>
        {action === "close"
          ? pending
            ? "Closing…"
            : "Close"
          : pending
            ? "Reopening…"
            : "Reopen"}
      </Button>
    </div>
  );
}

/**
 * DeleteTaskButton removes the task behind an inline confirmation, then
 * returns to the Task collection — the task this screen is about no longer
 * exists, so staying here would leave a dead page. Deletion also removes the
 * task's GitLab issue link; closing a task is the reversible option and stays
 * the prominent one.
 */
function DeleteTaskButton({ task }: { task: Task }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}`, {
        method: "DELETE",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to delete task.");
        setPending(false);
        return;
      }
      router.push(tasksPath(task.projectId));
      router.refresh();
    } catch {
      setPending(false);
    }
  }

  if (confirming) {
    return (
      <div className="flex flex-col items-end gap-1">
        {error ? <span className="text-destructive text-xs">{error}</span> : null}
        <span className="text-foreground text-xs">Delete this task?</span>
        <div className="flex gap-2">
          <Button variant="destructive" size="sm" onClick={handleDelete} disabled={pending}>
            {pending ? "Deleting…" : "Confirm delete"}
          </Button>
          <Button variant="outline" size="sm" onClick={() => setConfirming(false)} disabled={pending}>
            Cancel
          </Button>
        </div>
      </div>
    );
  }

  return (
    <Button variant="outline" size="sm" onClick={() => setConfirming(true)}>
      Delete
    </Button>
  );
}

const UNCLASSIFIED = "unclassified";

/** BacklogSelect assigns the task to a backlog, or back to Unclassified. */
function BacklogSelect({
  task,
  backlogs,
  onChanged,
}: {
  task: Task;
  backlogs: Backlog[];
  onChanged: (task: Task) => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const options = useMemo(
    () => [
      { value: UNCLASSIFIED, label: "Unclassified" },
      ...backlogs.map((b) => ({ value: b.id, label: b.name })),
    ],
    [backlogs],
  );

  async function handleChange(value: string) {
    const backlogId = value === UNCLASSIFIED ? null : value;

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}/assign-backlog`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ backlogId }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to assign backlog.");
        return;
      }
      onChanged((await res.json()) as Task);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col items-start gap-1">
      {error ? <span className="text-destructive text-xs">{error}</span> : null}
      <Combobox
        aria-label="Backlog"
        options={options}
        value={task.backlogId ?? UNCLASSIFIED}
        onChange={handleChange}
        disabled={pending}
        size="sm"
        className="w-52"
        searchPlaceholder="Search backlogs…"
        emptyText="No backlog found."
      />
    </div>
  );
}

/**
 * GitlabSyncSection shows a task's sync badge, a link to its GitLab issue
 * once one exists, and — only while failed — the error text plus a retry
 * action. The retry action belongs here, on the task, per docs/ui-design.md.
 */
function GitlabSyncSection({
  task,
  onChanged,
}: {
  task: Task;
  onChanged: (task: Task) => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const gitlab = task.gitlab;

  async function handleRetry() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/tasks/${task.id}/sync-retry`, {
        method: "POST",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to retry sync.");
        return;
      }
      onChanged((await res.json()) as Task);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <SyncBadge gitlab={gitlab} />
      {gitlab?.webUrl ? (
        <a
          href={gitlab.webUrl}
          target="_blank"
          rel="noreferrer"
          className="text-primary text-xs hover:underline"
        >
          View issue{gitlab.issueIid ? ` #${gitlab.issueIid}` : ""}
        </a>
      ) : null}
      {gitlab?.syncStatus === "failed" ? (
        <div className="border-destructive/50 bg-destructive/5 mt-1 flex w-full flex-col items-start gap-2 rounded-md border p-2 text-xs sm:flex-row sm:items-center sm:justify-between">
          <p className="text-destructive">{gitlab.lastError || "Sync failed."}</p>
          <div className="flex items-center gap-2">
            {error ? <span className="text-destructive">{error}</span> : null}
            <Button variant="outline" size="sm" onClick={handleRetry} disabled={pending}>
              {pending ? "Retrying…" : "Retry"}
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

/**
 * TaskDetail is the single view for one task, per docs/ui-design.md and the
 * order fixed in the issue: identity -> attributes -> AI-facing information.
 * Close/Reopen, backlog assignment, editing and deletion live here, on the
 * object they act on — there is no separate edit screen (rule 4); the link
 * back to the Task collection is the page's breadcrumb.
 *
 * Editing swaps the identity and attribute blocks for a form. The AI context
 * card below keeps its own edit toggle, since those fields are a separate
 * record that never reaches GitLab.
 */
export function TaskDetail({
  task: initial,
  backlogs,
  tasks,
  dependencies,
  assigneeOptions = null,
  labelOptions = null,
}: {
  task: Task;
  backlogs: Backlog[];
  // The project's tasks and every dependency in it: what the
  // predecessor/successor pickers choose from.
  tasks: Task[];
  dependencies: TaskDependency[];
  // The task's linked GitLab project's members/labels, or null when the
  // project has no default linked GitLab project — the edit form falls back
  // to free-text assignee/label entry in that case (issue #80).
  assigneeOptions?: GitlabMemberOption[] | null;
  labelOptions?: GitlabLabelOption[] | null;
}) {
  const [task, setTask] = useState(initial);
  const [editing, setEditing] = useState(false);

  return (
    <>
      <Card>
        {editing ? (
          <CardContent>
            <TaskEditForm
              task={task}
              backlogs={backlogs}
              assigneeOptions={assigneeOptions}
              labelOptions={labelOptions}
              onSaved={(updated) => {
                setTask(updated);
                setEditing(false);
              }}
              onCancel={() => setEditing(false)}
            />
          </CardContent>
        ) : (
          <>
            <CardHeader>
              <div className="flex items-start justify-between gap-4">
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h1 className="text-foreground text-xl leading-none font-semibold">
                      {task.title}
                    </h1>
                    <Badge variant={task.status === "open" ? "default" : "secondary"}>
                      {task.status === "open" ? "Open" : "Closed"}
                    </Badge>
                  </div>
                  {task.description ? (
                    <CardDescription className="mt-1.5 whitespace-pre-wrap">
                      {task.description}
                    </CardDescription>
                  ) : null}
                  <div className="mt-2">
                    <GitlabSyncSection task={task} onChanged={setTask} />
                  </div>
                </div>
                <div className="flex shrink-0 items-start gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                    Edit task
                  </Button>
                  <CloseReopenButton task={task} onChanged={setTask} />
                  <DeleteTaskButton task={task} />
                </div>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
                {/* Priority and progress are FlowLens's own axes, not the
                    GitLab issue state, so they sit with the other attributes
                    rather than beside the title. */}
                <div>
                  <dt className="text-muted-foreground">Priority</dt>
                  <dd className="text-foreground">
                    <PriorityBadge priority={task.priority} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Progress</dt>
                  <dd className="text-foreground">
                    <ProgressBadge progress={task.progress} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Assignee</dt>
                  <dd className="text-foreground">{task.assigneeGitlabUsername || "Unassigned"}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Labels</dt>
                  <dd className="text-foreground">
                    {task.labels.length > 0 ? (
                      <span className="flex flex-wrap gap-1">
                        {task.labels.map((label) => (
                          <Badge key={label} variant="outline">
                            {label}
                          </Badge>
                        ))}
                      </span>
                    ) : (
                      "No labels"
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Start</dt>
                  <dd className="text-foreground">
                    {task.startDate ? formatDate(task.startDate) : "No start date"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Due</dt>
                  <dd className="text-foreground">
                    {task.dueOn ? formatDate(task.dueOn) : "No due date"}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Backlog</dt>
                  <dd className="text-foreground">
                    <BacklogSelect task={task} backlogs={backlogs} onChanged={setTask} />
                  </dd>
                </div>
              </dl>
            </CardContent>
          </>
        )}
      </Card>

      <Card className="mt-8">
        <CardHeader>
          <CardTitle className="text-base font-medium">Dependencies</CardTitle>
          <CardDescription>
            Scheduling order within the project. Never synced to GitLab.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <TaskDependencySection task={task} tasks={tasks} dependencies={dependencies} />
        </CardContent>
      </Card>

      <Card className="mt-8">
        <CardHeader>
          <CardTitle className="text-base font-medium">AI-facing context</CardTitle>
          <CardDescription>
            What an AI coding agent needs to understand and scope this task.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <AIContextSection taskId={task.id} aiContext={task.aiContext} />
        </CardContent>
      </Card>
    </>
  );
}
