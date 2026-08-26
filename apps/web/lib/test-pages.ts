/**
 * taskPage wraps an array of tasks in the {tasks, nextPage, totalCount,
 * openCount} envelope getTasks/getAllTasks return, for the many screen tests
 * whose subject is what a screen does with the rows rather than how it pages.
 * Paging itself is asserted directly, with an explicit envelope, by the
 * collection screens' own tests.
 *
 * The rows are deliberately typed as loosely as the fixtures that feed them:
 * these tests hand the mocked API partial task literals ({ id: "t1" }) and
 * always have, so requiring a whole Task here would turn a test-only helper
 * into a reason to spell out twenty irrelevant fields. `status` is the only
 * field it reads, and only to derive openCount — hence isOpen's `in` check
 * rather than a constraint, which would make `status` the one property an
 * inferred literal is allowed to have.
 *
 * The counts default to the array's own length, which is what an unpaged
 * fixture means: one page holding everything that matched.
 */
export function taskPage<T extends object>(
  tasks: T[],
  overrides: { nextPage?: number; totalCount?: number; openCount?: number } = {},
) {
  return {
    tasks,
    nextPage: overrides.nextPage ?? 0,
    totalCount: overrides.totalCount ?? tasks.length,
    openCount: overrides.openCount ?? tasks.filter(isOpen).length,
  };
}

function isOpen(task: object): boolean {
  return "status" in task && task.status === "open";
}
