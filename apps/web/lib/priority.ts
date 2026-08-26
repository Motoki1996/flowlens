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
 * PRIORITY_OPTIONS is the same four, highest first — the order a *picker*
 * reads in: a select or filter menu is scanned top-down for the value you
 * mean, and the one you reach for is almost always the urgent end. It is
 * deliberately the reverse of PRIORITY_COLUMNS rather than a second opinion
 * about the axis: a board is a spatial ramp (left to right as rising
 * urgency), a menu is a ranked list, and Jira/Linear/GitLab all rank theirs
 * downwards. Every Select and filter menu uses this one; only the boards use
 * PRIORITY_COLUMNS.
 */
export const PRIORITY_OPTIONS: { priority: Priority; label: string }[] = [
  ...PRIORITY_COLUMNS,
].reverse();

/** PRIORITY_LABELS is PRIORITY_COLUMNS keyed by value, for the badge, the
 *  filter menus and the timeline tooltip, so one priority value reads the same
 *  wherever it appears. */
export const PRIORITY_LABELS: Record<Priority, string> = {
  low: "Low",
  medium: "Medium",
  high: "High",
  urgent: "Urgent",
};

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
