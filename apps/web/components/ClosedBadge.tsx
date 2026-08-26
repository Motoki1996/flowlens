import type { Status } from "@/types";
import { Badge } from "@/components/ui/badge";

/**
 * ClosedBadge marks a closed backlog or epic wherever one is still on screen —
 * its own single view, and a collection listing that was asked for closed
 * objects with ?status=.
 *
 * It renders nothing for an open object rather than an "Open" badge: open is
 * the overwhelming default, and a badge on every row would say nothing while
 * costing the eye something. Contrast a task, whose open/closed state is shown
 * both ways because it mirrors GitLab and is the first thing asked of a task.
 */
export function ClosedBadge({ status }: { status: Status }) {
  if (status === "open") return null;
  return <Badge variant="secondary">Closed</Badge>;
}
