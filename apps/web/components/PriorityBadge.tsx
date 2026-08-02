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
