"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { taskPath } from "@/lib/routes";
import type { ApiError, Task, TaskDependency } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";

type Direction = "predecessors" | "successors";

/**
 * TaskDependencySection edits one task's predecessor and successor edges from
 * the task's own single view. The Timeline view mode of the Task collection
 * renders the same edges, but only reads them.
 *
 * Cycles are rejected by the API, not here: the application layer owns that
 * check (internal/taskdependency), so this component only surfaces the error
 * it returns.
 */
export function TaskDependencySection({
  task,
  tasks,
  dependencies: initial,
}: {
  task: Task;
  tasks: Task[];
  dependencies: TaskDependency[];
}) {
  const [dependencies, setDependencies] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const titleOf = useMemo(() => {
    const byId = new Map(tasks.map((t) => [t.id, t]));
    return (id: string) => byId.get(id)?.title ?? "(unknown task)";
  }, [tasks]);

  const predecessors = dependencies.filter((d) => d.successorTaskId === task.id);
  const successors = dependencies.filter((d) => d.predecessorTaskId === task.id);

  // A task can neither depend on itself nor carry the same edge twice, so both
  // are kept out of the pickers rather than left to fail on submit.
  function optionsFor(direction: Direction) {
    const rows = direction === "predecessors" ? predecessors : successors;
    const taken = new Set(
      rows.map((d) => (direction === "predecessors" ? d.predecessorTaskId : d.successorTaskId)),
    );
    return tasks
      .filter((t) => t.id !== task.id && !taken.has(t.id))
      .map((t) => ({ value: t.id, label: t.title }));
  }

  async function handleAdd(direction: Direction, otherTaskId: string) {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/projects/${task.projectId}/task-dependencies`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(
            direction === "predecessors"
              ? { predecessorTaskId: otherTaskId, successorTaskId: task.id }
              : { predecessorTaskId: task.id, successorTaskId: otherTaskId },
          ),
        },
      );
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to add the dependency.");
        return;
      }
      const created = (await res.json()) as TaskDependency;
      setDependencies((current) => [...current, created]);
    } finally {
      setPending(false);
    }
  }

  async function handleRemove(dependencyId: string) {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/task-dependencies/${dependencyId}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to remove the dependency.");
        return;
      }
      setDependencies((current) => current.filter((d) => d.id !== dependencyId));
    } finally {
      setPending(false);
    }
  }

  // Written as a render function rather than a nested component: a nested
  // component is a new type on every render, which would remount the Combobox
  // and close its popover mid-interaction.
  function renderList(direction: Direction) {
    const rows = direction === "predecessors" ? predecessors : successors;
    const label = direction === "predecessors" ? "Predecessors" : "Successors";
    const otherId = (d: TaskDependency) =>
      direction === "predecessors" ? d.predecessorTaskId : d.successorTaskId;

    return (
      <div>
        <h3 className="text-foreground text-sm font-medium">{label}</h3>
        <p className="text-muted-foreground mt-0.5 text-xs">
          {direction === "predecessors"
            ? "Tasks that must finish before this one starts."
            : "Tasks that cannot start until this one finishes."}
        </p>
        {rows.length > 0 ? (
          <ul className="mt-2 space-y-1">
            {rows.map((d) => (
              <li key={d.id} className="flex items-center justify-between gap-2 text-sm">
                <Link
                  href={taskPath(task.projectId, otherId(d))}
                  className="text-primary hover:underline"
                >
                  {titleOf(otherId(d))}
                </Link>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleRemove(d.id)}
                  disabled={pending}
                  aria-label={`Remove ${titleOf(otherId(d))} from ${label.toLowerCase()}`}
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-muted-foreground mt-2 text-sm">None.</p>
        )}
        <Combobox
          aria-label={`Add ${label.toLowerCase()}`}
          options={optionsFor(direction)}
          value=""
          onChange={(value) => handleAdd(direction, value)}
          disabled={pending}
          size="sm"
          className="mt-2"
          placeholder={`Add ${direction === "predecessors" ? "a predecessor" : "a successor"}…`}
          searchPlaceholder="Search tasks…"
          emptyText="No task found."
        />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
        {renderList("predecessors")}
        {renderList("successors")}
      </div>
    </div>
  );
}
