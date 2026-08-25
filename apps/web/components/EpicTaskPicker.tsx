"use client";

import { useMemo, useState } from "react";
import { CheckIcon } from "lucide-react";
import type { Task } from "@/types";
import { Badge } from "@/components/ui/badge";
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";

/**
 * EpicTaskPicker is the "which tasks are in this epic" control, shared by the
 * epic's create/edit form and its single view — the two places the epic side
 * of the task↔epic relationship is edited. (The task side is the Epic control
 * on the task's own single view, and `epicId` on task creation.)
 *
 * It is a list to scan and tick, not a combobox to search one value out of:
 * the decision being made is "which of this backlog's tasks belong together",
 * which means reading the candidates, not recalling them. It is built on
 * cmdk (components/ui/command.tsx) inline — no popover — so the list comes
 * with arrow-key navigation and Enter-to-toggle, which is what makes a long
 * candidate list workable at all.
 *
 * Filtering is deliberately ours rather than cmdk's (`shouldFilter={false}`):
 * the bulk actions have to state how many rows they will affect, and a filter
 * that lives inside cmdk can't be counted from out here.
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
  const [search, setSearch] = useState("");
  // Closed tasks are hidden by default: in an established backlog they are
  // most of the candidates, and almost never what a new epic is cut out of.
  const [openOnly, setOpenOnly] = useState(true);
  const [selectedOnly, setSelectedOnly] = useState(false);

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

  // What the three filters leave on screen. Every count and bulk action below
  // is stated against exactly this list, never against `candidates`: an
  // action that reached rows the reader can't see would be the one thing a
  // filter must never do.
  const visible = useMemo(() => {
    const query = search.trim().toLowerCase();
    return candidates.filter((t) => {
      if (query && !t.title.toLowerCase().includes(query)) return false;
      // A closed task that is already ticked stays on screen even under
      // "Open only": the set is saved whole, so anything the reader has
      // picked has to remain reviewable. The filter is about narrowing what
      // to pick from, not about hiding what was picked.
      if (openOnly && t.status === "closed" && !selected.has(t.id)) return false;
      if (selectedOnly && !selected.has(t.id)) return false;
      return true;
    });
  }, [candidates, search, openOnly, selectedOnly, selected]);

  function toggle(taskId: string) {
    const next = new Set(selected);
    if (next.has(taskId)) {
      next.delete(taskId);
    } else {
      next.add(taskId);
    }
    onChange([...next]);
  }

  /** Adds every visible row to the selection, leaving anything selected but
   *  filtered out exactly where it is. */
  function selectAllVisible() {
    const next = new Set(selected);
    for (const t of visible) next.add(t.id);
    onChange([...next]);
  }

  /** Clears the visible rows only, for the same reason: a "Clear" that also
   *  dropped hidden selections would silently unfile tasks the reader never
   *  saw. */
  function clearVisible() {
    const next = new Set(selected);
    for (const t of visible) next.delete(t.id);
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

  const hiddenClosed = openOnly && candidates.some((t) => t.status === "closed");

  return (
    <div id={id} className="border-border overflow-hidden rounded-md border">
      <Command shouldFilter={false} className="bg-transparent">
        <CommandInput
          value={search}
          onValueChange={setSearch}
          placeholder="Search tasks…"
          aria-label="Search tasks"
          disabled={disabled}
        />

        <div className="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2 text-xs">
          <Badge variant="secondary">{selected.size} selected</Badge>
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              className="size-3.5"
              checked={openOnly}
              disabled={disabled}
              onChange={(e) => setOpenOnly(e.target.checked)}
            />
            Open only
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              className="size-3.5"
              checked={selectedOnly}
              disabled={disabled}
              onChange={(e) => setSelectedOnly(e.target.checked)}
            />
            Selected only
          </label>
          {/* Both counts name the visible rows, which is all these two reach. */}
          <button
            type="button"
            onClick={selectAllVisible}
            disabled={disabled || visible.length === 0}
            className="hover:text-foreground underline disabled:opacity-40 disabled:no-underline"
          >
            Select all ({visible.length})
          </button>
          <button
            type="button"
            onClick={clearVisible}
            disabled={disabled || visible.length === 0}
            className="hover:text-foreground underline disabled:opacity-40 disabled:no-underline"
          >
            Clear ({visible.length})
          </button>
        </div>

        <Separator />

        {/* cmdk owns `aria-selected` on its rows — it is the keyboard cursor,
            not the tick — so being in the epic is `aria-checked`, which the
            option role supports. Claiming aria-multiselectable as well would
            point a screen reader at aria-selected, which here means something
            else entirely. */}
        <CommandList className="max-h-80">
          <CommandEmpty>
            {selectedOnly
              ? "Nothing selected yet."
              : search.trim()
                ? `No tasks match "${search.trim()}".`
                : hiddenClosed
                  ? "No open tasks left. Untick “Open only” to see the closed ones."
                  : "No tasks to show."}
          </CommandEmpty>
          {visible.map((task) => {
            const isSelected = selected.has(task.id);
            return (
              <CommandItem
                // cmdk keys its rows by value, so the id goes in: two tasks
                // in one backlog can share a title.
                key={task.id}
                value={`${task.title} ${task.id}`}
                aria-checked={isSelected}
                disabled={disabled}
                onSelect={() => toggle(task.id)}
                className="cursor-pointer"
              >
                <span
                  aria-hidden
                  className={cn(
                    "border-primary flex size-4 shrink-0 items-center justify-center rounded-[4px] border",
                    isSelected ? "bg-primary text-primary-foreground" : "opacity-60",
                  )}
                >
                  {isSelected ? <CheckIcon className="size-3 text-current" /> : null}
                </span>
                <span className="truncate">{task.title}</span>
                {task.status === "closed" ? (
                  <span className="text-muted-foreground ml-auto shrink-0 text-xs">Closed</span>
                ) : null}
              </CommandItem>
            );
          })}
        </CommandList>
      </Command>
    </div>
  );
}

/** setEpicTasks is the one call both screens make: PATCH the epic's whole
 *  task set. Declarative and all-or-nothing — see the endpoint's own doc. */
export function epicTasksBody(taskIds: string[]) {
  return JSON.stringify({ taskIds });
}
