import type { Backlog } from "@/types";
import { groupScheduleLabel } from "@/lib/groups";

/** backlogScheduleLabel renders a backlog's planned period as one line. It is
 *  groupScheduleLabel narrowed to a Backlog — an epic renders its own period
 *  the same way, so the rule lives once in lib/groups.ts. */
export function backlogScheduleLabel(backlog: Backlog): string | null {
  return groupScheduleLabel(backlog);
}
