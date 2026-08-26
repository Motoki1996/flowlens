import type { Priority } from "@/types";
import { PRIORITY_ACCENT, PRIORITY_LABELS } from "@/lib/priority";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

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
 * PriorityDot is the same vocabulary reduced to its accent colour — the form
 * shown inside a picker (a Select item, a filter menu), where a full badge in
 * every row would compete with the field it is inside. The label always sits
 * beside it, so the dot is `aria-hidden`: colour is the scan aid, never the
 * only way to read the value.
 */
export function PriorityDot({ priority }: { priority: Priority }) {
  return (
    <span
      aria-hidden
      className={cn("size-2 shrink-0 rounded-full", PRIORITY_ACCENT[priority])}
      title={PRIORITY_LABELS[priority]}
    />
  );
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
