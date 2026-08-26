import type { Task, TaskWithProject } from "@/types";

/**
 * taskPage wraps an array of tasks in the {tasks, nextPage, totalCount,
 * openCount} envelope getTasks/getAllTasks return, for the many screen tests
 * whose subject is what a screen does with the rows rather than how it pages.
 * Paging itself is asserted directly, with an explicit envelope, by the
 * collection screens' own tests.
 *
 * The counts default to the array's own length, which is what an unpaged
 * fixture means: one page holding everything that matched.
 */
export function taskPage<T extends Task | TaskWithProject>(
  tasks: T[],
  overrides: { nextPage?: number; totalCount?: number; openCount?: number } = {},
) {
  return {
    tasks,
    nextPage: overrides.nextPage ?? 0,
    totalCount: overrides.totalCount ?? tasks.length,
    openCount: overrides.openCount ?? tasks.filter((t) => t.status === "open").length,
  };
}
