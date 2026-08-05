import type { Progress } from "@/types";

/**
 * The columns of a progress board, in the order work advances — the axis reads
 * left to right as the task moving towards done, with On hold sitting between
 * In progress and Done because a held task is work already started. Shared by
 * the Backlog and Task boards so the two never disagree on which way it points,
 * and by the list sort, which orders by the same rank.
 */
export const PROGRESS_COLUMNS: { progress: Progress; label: string }[] = [
  { progress: "not_started", label: "Not started" },
  { progress: "in_progress", label: "In progress" },
  { progress: "on_hold", label: "On hold" },
  { progress: "done", label: "Done" },
];

/** PROGRESS_LABELS is PROGRESS_COLUMNS keyed by value, for the badge and the
 *  filter menus, so one progress value reads the same wherever it appears. */
export const PROGRESS_LABELS: Record<Progress, string> = {
  not_started: "Not started",
  in_progress: "In progress",
  on_hold: "On hold",
  done: "Done",
};

/**
 * Each column's accent dot, so a card's progress survives being read out of the
 * column header's context and the board never relies on position alone.
 * Deliberately the same vocabulary as ProgressBadge.
 */
export const PROGRESS_ACCENT: Record<Progress, string> = {
  not_started: "bg-muted-foreground/30",
  in_progress: "bg-primary",
  on_hold: "bg-destructive",
  done: "bg-secondary-foreground/40",
};
