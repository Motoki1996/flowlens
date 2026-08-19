import type { Size } from "@/types";
import { SIZE_LABELS, SIZE_POINTS } from "@/lib/size";
import { Badge } from "@/components/ui/badge";

/**
 * SizeBadge shows a task's size — app-only, never synced to GitLab (see the
 * "Task size" section in README.md). Used in list rows and the task single
 * view, so both read a given size the same way.
 *
 * Unlike PriorityBadge, every size renders in the same neutral outline
 * variant rather than escalating colour: size is not urgency, and a red XL
 * would read as "this is a problem" when it only means "this is big". The
 * weight is spelled out in the title attribute so the number behind velocity
 * is discoverable without cluttering the row.
 */
export function SizeBadge({ size }: { size: Size }) {
  return (
    <Badge variant="outline" className="font-mono tabular-nums" title={`Size ${SIZE_LABELS[size]} — ${SIZE_POINTS[size]} points`}>
      {SIZE_LABELS[size]}
    </Badge>
  );
}
