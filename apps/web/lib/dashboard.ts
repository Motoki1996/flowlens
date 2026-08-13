// Pure date-math for a task's due-date state: originally just the
// dashboard's own overdue/due-soon split (issue #77), now shared with the
// project Task collection's due-date filter and badge (issue #148) so the
// two screens never disagree on what "overdue" or "this week" means. Kept
// separate from any component so the day-boundary logic is unit-testable
// without rendering anything — the same seam lib/timeline.ts uses for the
// Gantt chart's date math.

import { startOfDay } from "@/lib/timeline";

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

/**
 * DueStatus classifies a single due date against `now`: "overdue" (before
 * today), "dueSoon" (today through the end of this week), "later" (due after
 * this week), or "undated" (no due date at all). The dashboard only ever
 * cares about the first two (see classifyDueTasks below); the Task
 * collection's `?due=` filter (issue #148) is what tells "later" and
 * "undated" apart, since "no due date" is its own filter value.
 */
export type DueStatus = "overdue" | "dueSoon" | "later" | "undated";

export function dueStatus(dueOn: string | null, now: Date): DueStatus {
  if (!dueOn) return "undated";
  const today = startOfDay(now);
  const dueDate = startOfDay(new Date(dueOn));
  if (dueDate.getTime() < today.getTime()) return "overdue";
  if (dueDate.getTime() <= endOfWeek(now).getTime()) return "dueSoon";
  return "later";
}

export interface DueTaskSections<T> {
  overdue: T[];
  dueSoon: T[];
}

/**
 * classifyDueTasks splits open tasks into "overdue" and "dueSoon" buckets
 * (see dueStatus above); undated and later-than-this-week tasks land in
 * neither — an undated task is not a due-date signal to surface here, and
 * the dashboard's empty state hints at setting one instead. Generic over T
 * so both the dashboard's TaskWithProject[] and the Task collection's plain
 * Task[] can share this one cutoff.
 */
export function classifyDueTasks<T extends { dueOn: string | null }>(
  tasks: T[],
  now: Date,
): DueTaskSections<T> {
  const overdue: T[] = [];
  const dueSoon: T[] = [];
  for (const task of tasks) {
    switch (dueStatus(task.dueOn, now)) {
      case "overdue":
        overdue.push(task);
        break;
      case "dueSoon":
        dueSoon.push(task);
        break;
    }
  }
  return { overdue, dueSoon };
}
