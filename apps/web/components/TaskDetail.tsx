"use client";

import { useState } from "react";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, Backlog, Task } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { AIContextSection } from "@/components/AIContextSection";

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

/**
 * TaskDetail is the single view for one task, per docs/ui-design.md and the
 * order fixed in the issue: identity -> attributes -> AI-facing information
 * -> related links. Close/Reopen live here, on the object they act on.
 */
export function TaskDetail({
  task: initial,
  project,
  backlog,
}: {
  task: Task;
  project: { id: string; name: string };
  backlog: Backlog | null;
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
                {/* Placeholder until GitLab issue sync ships (task.gitlab is always null today). */}
                <Badge variant="outline">Not synced to GitLab</Badge>
              </div>
              {task.description ? (
                <CardDescription className="mt-1.5 whitespace-pre-wrap">
                  {task.description}
                </CardDescription>
              ) : null}
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
              <dd className="text-foreground">{backlog ? backlog.name : "未分類"}</dd>
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
