import type { Priority } from "@/types";
import { Badge } from "@/components/ui/badge";

/**
 * PriorityBadge shows a task's or backlog's priority, app-only and never
 * synced to GitLab (see the "Task & backlog priority" section in README.md).
 * Used in list rows, timeline name columns and the task single view, so all
 * three never disagree on what a given priority reads as.
 */
export function PriorityBadge({ priority }: { priority: Priority }) {
  switch (priority) {
    case "urgent":
      return (
        <Badge variant="outline" className="border-destructive/50 text-destructive">
          Urgent
        </Badge>
      );
    case "high":
      return <Badge variant="default">High</Badge>;
    case "medium":
      return <Badge variant="secondary">Medium</Badge>;
    case "low":
      return <Badge variant="outline">Low</Badge>;
  }
}

/**
 * PriorityFlag is the same badge, shown selectively — for the Gantt timeline's
 * name column, where the pill sits on its own line under the title rather than
 * beside it (beside it, the title was left a few dozen pixels and every row
 * read as an ellipsis).
 *
 * It is deliberately silent below `high`: `medium` is the default and `low` is
 * not worth interrupting a scan for, so the badge appears only when the
 * priority is the reason to look at the row. Every priority is still stated on
 * the bar's tooltip, so nothing is only available here.
 */
export function PriorityFlag({ priority }: { priority: Priority }) {
  if (priority !== "high" && priority !== "urgent") return null;
  return <PriorityBadge priority={priority} />;
}
