import type { Progress } from "@/types";
import { Badge } from "@/components/ui/badge";

/**
 * ProgressBadge shows a task's or backlog's progress — FlowLens's own
 * four-stage work state, app-only and never synced to GitLab (see the "Task &
 * backlog progress" section in README.md). It sits alongside, never instead
 * of, a task's status badge: status is the GitLab issue state, progress is
 * ours, and a task can be closed while still reading On hold.
 *
 * Used in list rows, timeline name columns and the single views, so all three
 * never disagree on what a given progress reads as.
 */
export function ProgressBadge({ progress }: { progress: Progress }) {
  switch (progress) {
    case "in_progress":
      return <Badge variant="default">In progress</Badge>;
    case "on_hold":
      // Amber, not destructive: `destructive` is reserved for things that have
      // actually gone wrong (a failed sync, an overdue bar), and held work is
      // paused rather than broken.
      return (
        <Badge variant="outline" className="border-chart-4/50 text-chart-4">
          On hold
        </Badge>
      );
    case "done":
      return <Badge variant="secondary">Done</Badge>;
    case "not_started":
      return <Badge variant="outline">Not started</Badge>;
  }
}
