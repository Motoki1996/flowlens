import type { Backlog } from "@/types";
import { formatDate } from "@/lib/dates";

/** backlogScheduleLabel renders a backlog's planned period as one line. Shared
 *  by the Backlog collection's List and Board view modes so the same backlog
 *  never reads differently between them. */
export function backlogScheduleLabel(backlog: Backlog): string | null {
  if (backlog.startDate && backlog.dueOn) {
    return `${formatDate(backlog.startDate)} – ${formatDate(backlog.dueOn)}`;
  }
  if (backlog.startDate) return `From ${formatDate(backlog.startDate)}`;
  if (backlog.dueOn) return `Due ${formatDate(backlog.dueOn)}`;
  return null;
}
