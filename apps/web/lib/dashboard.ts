// Pure date-math for the dashboard's due-date sections (issue #77). Kept
// separate from the page/component so the day-boundary logic is
// unit-testable without rendering anything — the same seam lib/timeline.ts
// uses for the Gantt chart's date math.

import { startOfDay } from "@/lib/timeline";
import type { TaskWithProject } from "@/types";

/**
 * endOfWeek returns the last day (Sunday) of the Monday–Sunday week `date`
 * falls in, at local midnight. Sunday itself is the end of its own week.
 * This is the dashboard's own decision for "this week" — the codebase has
 * no other week-boundary convention to match.
 */
export function endOfWeek(date: Date): Date {
  const start = startOfDay(date);
  const day = start.getDay(); // 0 = Sunday, 1 = Monday, ... 6 = Saturday
  const daysUntilSunday = day === 0 ? 0 : 7 - day;
  return new Date(start.getFullYear(), start.getMonth(), start.getDate() + daysUntilSunday);
}

export interface DueTaskSections {
  overdue: TaskWithProject[];
  dueSoon: TaskWithProject[];
}

/**
 * classifyDueTasks splits open tasks into "overdue" (due before today) and
 * "dueSoon" (due today through the end of this week). Tasks with no due date
 * land in neither — an undated task is not a due-date signal to surface
 * here, and the dashboard's empty state hints at setting one instead.
 */
export function classifyDueTasks(tasks: TaskWithProject[], now: Date): DueTaskSections {
  const today = startOfDay(now);
  const weekEnd = endOfWeek(now);
  const overdue: TaskWithProject[] = [];
  const dueSoon: TaskWithProject[] = [];
  for (const task of tasks) {
    if (!task.dueOn) continue;
    const dueDate = startOfDay(new Date(task.dueOn));
    if (dueDate.getTime() < today.getTime()) {
      overdue.push(task);
    } else if (dueDate.getTime() <= weekEnd.getTime()) {
      dueSoon.push(task);
    }
  }
  return { overdue, dueSoon };
}
