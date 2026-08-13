import { dueStatus } from "@/lib/dashboard";
import { formatDate } from "@/lib/dates";
import { cn } from "@/lib/utils";

/**
 * DueDateLabel renders a task's due date the same way everywhere it's shown
 * next to other row/card meta info (issue #148): overdue is
 * `text-destructive` *and* says "Overdue" rather than relying on color
 * alone, using the same cutoff `lib/dashboard.ts`'s dueStatus already gives
 * the dashboard, so a task never reads as overdue on one screen and merely
 * "due" on another.
 */
export function DueDateLabel({ dueOn, now }: { dueOn: string; now: Date }) {
  const overdue = dueStatus(dueOn, now) === "overdue";
  return (
    <span className={cn(overdue && "text-destructive font-medium")}>
      {overdue ? "Overdue" : "Due"} {formatDate(dueOn)}
    </span>
  );
}
