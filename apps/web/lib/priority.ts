import type { Priority } from "@/types";

/**
 * The columns of a priority board, lowest first — the axis reads left to right
 * as rising urgency. Shared by the Backlog and Task boards so the two never
 * disagree on which way it points.
 */
export const PRIORITY_COLUMNS: { priority: Priority; label: string }[] = [
  { priority: "low", label: "Low" },
  { priority: "medium", label: "Medium" },
  { priority: "high", label: "High" },
  { priority: "urgent", label: "Urgent" },
];

/**
 * Each column's accent dot, so a card's priority survives being read out of the
 * column header's context and the board never relies on position alone.
 * Deliberately the same vocabulary as PriorityBadge.
 */
export const PRIORITY_ACCENT: Record<Priority, string> = {
  urgent: "bg-destructive",
  high: "bg-primary",
  medium: "bg-secondary-foreground/40",
  low: "bg-muted-foreground/30",
};
