"use client";

import { useState, type ReactNode } from "react";
import type { Priority, Progress } from "@/types";
import { PRIORITY_OPTIONS } from "@/lib/priority";
import { PROGRESS_COLUMNS } from "@/lib/progress";
import { Button } from "@/components/ui/button";
import { PriorityDot } from "@/components/PriorityBadge";
import { ProgressDot } from "@/components/ProgressBadge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { BulkSelection } from "@/components/BulkSelection";

/** What the bar actually reads off a selection — narrowed from
 *  BulkSelection so a screen whose action union is wider than
 *  BaseBulkAction (every one of them, since each adds its own) still fits. */
type BarSelection = Pick<
  BulkSelection<string>,
  "selected" | "error" | "clear"
> & { pending: string | null };

/**
 * BulkActionBar is the selection bar every collection List view shows once
 * something is selected: the count, the priority/progress pickers with their
 * apply buttons, close/reopen, and Cancel.
 *
 * The four actions it owns are the ones a Task, a Backlog and an Epic all
 * have in common — each carries its own `priority`, `progress` and
 * open/closed `status`. A screen's own extra action goes in `children`,
 * which renders directly after the count: the Task collection's "Assign to
 * backlog" and the Epic collection's "Move to backlog" are the two today.
 *
 * Each handler resolves to the ids that failed (what BulkSelection.run
 * returns), which is what tells the bar whether to reset its picker — a
 * partial failure leaves the chosen value in place so the retry is one click
 * rather than a re-pick.
 */
export function BulkActionBar({
  selection,
  onPriority,
  onProgress,
  onClose,
  onReopen,
  children,
}: {
  selection: BarSelection;
  onPriority: (priority: Priority) => Promise<string[]>;
  onProgress: (progress: Progress) => Promise<string[]>;
  onClose: () => void | Promise<unknown>;
  onReopen: () => void | Promise<unknown>;
  children?: ReactNode;
}) {
  const [priority, setPriority] = useState<Priority | "">("");
  const [progress, setProgress] = useState<Progress | "">("");
  const { selected, pending, error, clear } = selection;

  if (selected.size === 0) return null;

  async function applyPriority() {
    if (!priority) return;
    const failed = await onPriority(priority);
    if (failed.length === 0) setPriority("");
  }

  async function applyProgress() {
    if (!progress) return;
    const failed = await onProgress(progress);
    if (failed.length === 0) setProgress("");
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {error ? <span className="text-destructive text-xs">{error}</span> : null}
      <span className="text-muted-foreground text-xs">
        {selected.size} selected
      </span>
      {children}
      <Select
        value={priority}
        onValueChange={(value) => setPriority(value as Priority)}
      >
        <SelectTrigger size="sm" aria-label="Priority to set" className="w-36">
          <SelectValue placeholder="Set priority…" />
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
      <Button
        size="sm"
        onClick={applyPriority}
        disabled={!priority || pending !== null}
      >
        {pending === "priority" ? "Setting…" : "Set priority"}
      </Button>
      <Select
        value={progress}
        onValueChange={(value) => setProgress(value as Progress)}
      >
        <SelectTrigger size="sm" aria-label="Progress to set" className="w-36">
          <SelectValue placeholder="Set progress…" />
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
      <Button
        size="sm"
        onClick={applyProgress}
        disabled={!progress || pending !== null}
      >
        {pending === "progress" ? "Setting…" : "Set progress"}
      </Button>
      {/* Both stay offered regardless of the selection's current mix of
          open/closed objects — closing an already-closed one (or reopening an
          already-open one) is a no-op server-side, the same thing the
          single-object CloseReopenButton relies on. */}
      <Button
        variant="outline"
        size="sm"
        onClick={onClose}
        disabled={pending !== null}
      >
        {pending === "close" ? "Closing…" : "Close selected"}
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={onReopen}
        disabled={pending !== null}
      >
        {pending === "reopen" ? "Reopening…" : "Reopen selected"}
      </Button>
      <Button
        variant="outline"
        size="sm"
        onClick={clear}
        disabled={pending !== null}
      >
        Cancel
      </Button>
    </div>
  );
}
