import type { Backlog, Epic } from "@/types";
import { backlogPath, epicPath, tasksPath } from "@/lib/routes";
import { formatDate } from "@/lib/dates";

/**
 * A "grouping" is a Backlog or an Epic — the two objects that group tasks and
 * are presented the same way (Board / List / Timeline, docs/ui-design.md rule
 * 5). An epic is deliberately shaped as a backlog that lives inside a backlog
 * (ADR-0012), so the view modes that only need a name, a schedule, a
 * priority/progress and a task ratio can serve both from one implementation
 * rather than two copies drifting apart.
 *
 * Only the parts that genuinely differ live here: what the object is called,
 * where its single view is, and which endpoint a PATCH goes to.
 */
export type GroupKind = "backlog" | "epic";

/** The shape both Backlog and Epic satisfy. Anything either one carries that
 *  the other doesn't (an epic's backlogId) is deliberately absent: a view
 *  written against this can't accidentally depend on it. */
export type Grouping = Pick<
  Backlog & Epic,
  | "id"
  | "projectId"
  | "name"
  | "description"
  | "startDate"
  | "dueOn"
  | "priority"
  | "progress"
  | "taskCount"
  | "closedTaskCount"
>;

export interface GroupConfig {
  /** Lower-case singular, used in sentences ("No backlogs."). */
  noun: string;
  /** Lower-case plural, used in aria-labels ("Done epics"). */
  plural: string;
  /** The object's single view. */
  detailPath: (projectId: string, id: string) => string;
  /** The Task collection, pre-filtered to this object. */
  tasksPath: (projectId: string, id: string) => string;
  /** The API resource a PATCH/DELETE for one of these goes to. */
  apiPath: (id: string) => string;
  /** The API collection a POST for these goes to. */
  collectionApiPath: (projectId: string) => string;
}

export const GROUP_CONFIG: Record<GroupKind, GroupConfig> = {
  backlog: {
    noun: "backlog",
    plural: "backlogs",
    detailPath: backlogPath,
    tasksPath: (projectId, id) => tasksPath(projectId, { backlogId: id }),
    apiPath: (id) => `/api/v1/backlogs/${id}`,
    collectionApiPath: (projectId) => `/api/v1/projects/${projectId}/backlogs`,
  },
  epic: {
    noun: "epic",
    plural: "epics",
    detailPath: epicPath,
    tasksPath: (projectId, id) => tasksPath(projectId, { epicId: id }),
    apiPath: (id) => `/api/v1/epics/${id}`,
    collectionApiPath: (projectId) => `/api/v1/projects/${projectId}/epics`,
  },
};

/** groupScheduleLabel renders a grouping's planned period as one line, so the
 *  same object never reads differently between the List and Board modes. */
export function groupScheduleLabel(item: {
  startDate: string | null;
  dueOn: string | null;
}): string | null {
  if (item.startDate && item.dueOn) {
    return `${formatDate(item.startDate)} – ${formatDate(item.dueOn)}`;
  }
  if (item.startDate) return `From ${formatDate(item.startDate)}`;
  if (item.dueOn) return `Due ${formatDate(item.dueOn)}`;
  return null;
}
