"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, Backlog, Task } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { AIContextSection } from "@/components/AIContextSection";
import { SyncBadge } from "@/components/SyncBadge";

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

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

const UNCLASSIFIED = "unclassified";

/** BacklogSelect assigns the task to a backlog, or back to 未分類. */
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
      { value: UNCLASSIFIED, label: "未分類" },
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
        headers: { "Content-Type": "application/json" },
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
 * order fixed in the issue: identity -> attributes -> AI-facing information
 * -> related links. Close/Reopen and backlog assignment live here, on the
 * object they act on.
 */
export function TaskDetail({
  task: initial,
  project,
  backlogs,
}: {
  task: Task;
  project: { id: string; name: string };
  backlogs: Backlog[];
}) {
  const [task, setTask] = useState(initial);

  return (
    <>
      <Card>
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
            <CloseReopenButton task={task} onChanged={setTask} />
          </div>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
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
              <dt className="text-muted-foreground">Due</dt>
              <dd className="text-foreground">{task.dueOn ? formatDate(task.dueOn) : "No due date"}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Backlog</dt>
              <dd className="text-foreground">
                <BacklogSelect task={task} backlogs={backlogs} onChanged={setTask} />
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card className="mt-8">
        <CardHeader>
          <CardTitle className="text-base font-medium">AI向け情報</CardTitle>
          <CardDescription>
            What an AI coding agent needs to understand and scope this task.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <AIContextSection taskId={task.id} aiContext={task.aiContext} />
        </CardContent>
      </Card>

      <div className="mt-8">
        <Link href={`/projects/${project.id}`} className="text-primary text-sm hover:underline">
          ← {project.name}
        </Link>
      </div>
    </>
  );
}
