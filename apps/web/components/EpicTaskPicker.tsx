"use client";

import { useMemo } from "react";
import type { Task } from "@/types";

/**
 * EpicTaskPicker is the "which tasks are in this epic" control, shared by the
 * epic's create/edit form and its single view — the two places the epic side
 * of the task↔epic relationship is edited. (The task side is the Epic control
 * on the task's own single view, and `epicId` on task creation.)
 *
 * It is a checkbox list rather than a multi-select combobox: the decision is
 * "which of this backlog's tasks belong together", which means reading the
 * candidates as a list, not searching for them one at a time.
 *
 * Candidates are the tasks of `backlogId` that are free to be filed here —
 * unfiled tasks plus the ones already in this epic. A task already in a
 * *different* epic is deliberately absent: moving it would silently take it
 * out of that epic, which is a decision to make on the task itself.
 */
export function EpicTaskPicker({
  id,
  tasks,
  backlogId,
  epicId,
  value,
  onChange,
  disabled = false,
}: {
  id: string;
  /** The project's tasks. Filtered to the candidates here rather than by the
   *  caller, so both screens offer exactly the same set. */
  tasks: Task[];
  /** The epic's backlog. null — an epic in no backlog — has no candidates:
   *  filing a task here would have nowhere to move it to. */
  backlogId: string | null;
  /** The epic being edited, or undefined while creating one. */
  epicId?: string;
  /** The selected task ids. */
  value: string[];
  onChange: (taskIds: string[]) => void;
  disabled?: boolean;
}) {
  const candidates = useMemo(
    () =>
      backlogId === null
        ? []
        : tasks.filter(
            (t) =>
              t.backlogId === backlogId && (t.epicId === null || (epicId && t.epicId === epicId)),
          ),
    [tasks, backlogId, epicId],
  );

  const selected = useMemo(() => new Set(value), [value]);

  function toggle(taskId: string) {
    const next = new Set(selected);
    if (next.has(taskId)) {
      next.delete(taskId);
    } else {
      next.add(taskId);
    }
    onChange([...next]);
  }

  if (backlogId === null) {
    return (
      <p className="text-muted-foreground text-sm">
        An epic outside a backlog has no tasks to draw from. File it in a backlog first.
      </p>
    );
  }

  if (candidates.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        No free tasks in this backlog. Tasks already in another epic aren&apos;t offered here —
        move them from the task itself.
      </p>
    );
  }

  return (
    <ul
      id={id}
      role="group"
      aria-label="Tasks in this epic"
      className="border-border max-h-64 divide-y overflow-y-auto rounded-md border"
    >
      {candidates.map((task) => (
        <li key={task.id}>
          <label className="hover:bg-accent/50 flex cursor-pointer items-center gap-2 px-3 py-2 text-sm">
            <input
              type="checkbox"
              className="size-4"
              checked={selected.has(task.id)}
              disabled={disabled}
              onChange={() => toggle(task.id)}
            />
            <span className="text-foreground truncate">{task.title}</span>
            {task.status === "closed" ? (
              <span className="text-muted-foreground ml-auto shrink-0 text-xs">Closed</span>
            ) : null}
          </label>
        </li>
      ))}
    </ul>
  );
}

/** setEpicTasks is the one call both screens make: PATCH the epic's whole
 *  task set. Declarative and all-or-nothing — see the endpoint's own doc. */
export function epicTasksBody(taskIds: string[]) {
  return JSON.stringify({ taskIds });
}
