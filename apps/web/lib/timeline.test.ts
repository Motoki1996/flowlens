import { describe, it, expect } from "vitest";
import {
  backlogCompletion,
  backlogTaskCompletion,
  computeAxis,
  computeTimelineBounds,
  defaultZoom,
  effectiveRange,
  formatAxisTick,
  hasSchedule,
  MIN_PLOT_WIDTH,
  plotWidth,
  spanDays,
  startOfDay,
  toBacklogGanttRows,
  toTaskGanttRows,
  todayOffset,
} from "./timeline";
import type { Backlog, Task } from "@/types";

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

/** boundsOf builds a plotted range of a known length, which is what the axis,
 *  the zoom default and the plot width are all decided from. */
const boundsOf = (startDay: string, days: number) => ({
  start: startOfDay(new Date(startDay)),
  end: new Date(startOfDay(new Date(startDay)).getTime() + days * ONE_DAY_MS),
});

/** day formats a Date the way the local-midnight helpers produce it, so the
 *  assertions read as dates rather than as timestamps. */
function day(date: Date | undefined) {
  if (!date) return undefined;
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${d}`;
}

function makeTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    projectId: "p1",
    backlogId: null,
    epicId: null,
    title: "Task",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    labels: [],
    dueOn: null,
    startDate: null,
    priority: "medium",
    progress: "not_started",
    size: "m",
    designStartedAt: null,
    implementationStartedAt: null,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      updatedAt: null,
    },
    ...overrides,
  };
}

describe("effectiveRange", () => {
  it("returns null when neither startDate nor dueOn is set", () => {
    expect(effectiveRange({ startDate: null, dueOn: null })).toBeNull();
  });

  it("uses dueOn alone as a single-day range", () => {
    const r = effectiveRange({ startDate: null, dueOn: "2026-08-05" });
    expect(r?.start).toEqual(r?.end);
  });

  it("uses startDate alone as a single-day range", () => {
    const r = effectiveRange({ startDate: "2026-08-01", dueOn: null });
    expect(r?.start).toEqual(r?.end);
  });

  it("spans startDate to dueOn when both are set", () => {
    const r = effectiveRange({ startDate: "2026-08-01", dueOn: "2026-08-05" });
    expect(day(r?.start)).toBe("2026-08-01");
    expect(day(r?.end)).toBe("2026-08-05");
  });

  it("swaps a dueOn set before startDate so the range is never inverted", () => {
    const r = effectiveRange({ startDate: "2026-08-05", dueOn: "2026-08-01" });
    expect(day(r?.start)).toBe("2026-08-01");
    expect(day(r?.end)).toBe("2026-08-05");
  });
});

describe("hasSchedule", () => {
  it("is false with neither date set", () => {
    expect(hasSchedule({ startDate: null, dueOn: null })).toBe(false);
  });

  it("is true with only one date set", () => {
    expect(hasSchedule({ startDate: "2026-08-01", dueOn: null })).toBe(true);
  });
});

describe("computeTimelineBounds", () => {
  it("returns null when no task has a schedule", () => {
    expect(computeTimelineBounds([{ startDate: null, dueOn: null }])).toBeNull();
  });

  it("pads a day either side and runs to the end of the last scheduled day", () => {
    const bounds = computeTimelineBounds([
      { startDate: "2026-08-01", dueOn: "2026-08-03" },
      { startDate: "2026-08-10", dueOn: null },
    ]);
    expect(day(bounds?.start)).toBe("2026-07-31");
    // 08-10 occupies the whole of that day (ending 08-11), plus a day of padding.
    expect(day(bounds?.end)).toBe("2026-08-12");
  });

  it("ignores unscheduled tasks mixed in with scheduled ones", () => {
    const bounds = computeTimelineBounds([
      { startDate: null, dueOn: null },
      { startDate: "2026-08-01", dueOn: "2026-08-01" },
    ]);
    expect(day(bounds?.start)).toBe("2026-07-31");
    expect(day(bounds?.end)).toBe("2026-08-03");
  });

  it("snaps to day boundaries so ticks land on midnight", () => {
    const bounds = computeTimelineBounds([{ startDate: "2026-08-01T13:45:00Z", dueOn: null }]);
    expect(bounds?.start.getHours()).toBe(0);
    expect(bounds?.end.getHours()).toBe(0);
  });
});

describe("computeAxis", () => {
  it.each([
    { days: 10, granularity: "day" },
    { days: 60, granularity: "week" },
    { days: 200, granularity: "month" },
  ])("uses $granularity ticks over $days days", ({ days, granularity }) => {
    expect(computeAxis(boundsOf("2026-08-03", days)).granularity).toBe(granularity);
  });

  it("emits one tick per day at day granularity", () => {
    expect(computeAxis(boundsOf("2026-08-03", 5)).ticks).toEqual([
      0,
      ONE_DAY_MS,
      2 * ONE_DAY_MS,
      3 * ONE_DAY_MS,
      4 * ONE_DAY_MS,
    ]);
  });

  it("snaps weekly ticks to Mondays rather than to the range start", () => {
    // 2026-08-05 is a Wednesday; the first Monday at or after it is 2026-08-10.
    const bounds = boundsOf("2026-08-05", 30);
    const axis = computeAxis(bounds);
    const firstTick = new Date(bounds.start.getTime() + axis.ticks[0]);
    expect(day(firstTick)).toBe("2026-08-10");
    expect(firstTick.getDay()).toBe(1);
  });

  it("snaps monthly ticks to the first of each month", () => {
    const bounds = boundsOf("2026-08-15", 200);
    const axis = computeAxis(bounds);
    const days = axis.ticks.map((t) => new Date(bounds.start.getTime() + t).getDate());
    expect(days.every((d) => d === 1)).toBe(true);
  });

  it("uses the given zoom instead of the span's own granularity", () => {
    // A 200-day span would derive month ticks; a reader who zoomed in gets days.
    expect(computeAxis(boundsOf("2026-08-03", 200), "day").granularity).toBe("day");
    // …and one who zoomed out of a short span gets months.
    expect(computeAxis(boundsOf("2026-08-03", 10), "month").granularity).toBe("month");
  });

  it("keeps every tick inside the plotted range", () => {
    const bounds = boundsOf("2026-08-03", 40);
    const total = bounds.end.getTime() - bounds.start.getTime();
    for (const tick of computeAxis(bounds).ticks) {
      expect(tick).toBeGreaterThanOrEqual(0);
      expect(tick).toBeLessThan(total);
    }
  });
});

describe("defaultZoom", () => {
  it.each([
    { days: 10, zoom: "day" },
    { days: 60, zoom: "week" },
    { days: 200, zoom: "month" },
  ])("opens a $days-day span at $zoom zoom", ({ days, zoom }) => {
    expect(defaultZoom(boundsOf("2026-08-03", days))).toBe(zoom);
  });
});

describe("plotWidth", () => {
  it("widens as the zoom gets finer, so the same range is scrolled not squeezed", () => {
    const bounds = boundsOf("2026-08-03", 200);
    expect(plotWidth(bounds, "day")).toBeGreaterThan(plotWidth(bounds, "week"));
    expect(plotWidth(bounds, "week")).toBeGreaterThan(plotWidth(bounds, "month"));
  });

  it("never draws narrower than the minimum, however short the range", () => {
    expect(plotWidth(boundsOf("2026-08-03", 3), "month")).toBe(MIN_PLOT_WIDTH);
  });
});

describe("formatAxisTick", () => {
  const bounds = { start: startOfDay(new Date("2026-08-01")), end: startOfDay(new Date("2027-01-01")) };

  it("omits the year on day and week ticks", () => {
    expect(formatAxisTick(0, bounds, "day")).not.toMatch(/2026/);
  });

  it("names the year on month ticks", () => {
    expect(formatAxisTick(0, bounds, "month")).toMatch(/2026/);
  });
});

describe("toTaskGanttRows", () => {
  const bounds = { start: startOfDay(new Date("2026-08-01")), end: startOfDay(new Date("2026-08-21")) };
  const now = new Date("2026-08-10T09:00:00Z");

  it("drops tasks with no schedule", () => {
    const rows = toTaskGanttRows([makeTask({ id: "t1" })], bounds, now);
    expect(rows).toEqual([]);
  });

  it("offsets a bar from the range start and spans whole days inclusively", () => {
    const [row] = toTaskGanttRows(
      [makeTask({ startDate: "2026-08-03", dueOn: "2026-08-05" })],
      bounds,
      now,
    );
    expect(row.offset).toBe(2 * ONE_DAY_MS);
    // 08-03 through 08-05 inclusive is three days wide, not two.
    expect(row.duration).toBe(3 * ONE_DAY_MS);
  });

  it("gives a single-day task a full day of width", () => {
    const [row] = toTaskGanttRows([makeTask({ dueOn: "2026-08-05" })], bounds, now);
    expect(row.duration).toBe(ONE_DAY_MS);
  });

  it("orders rows by start date", () => {
    const rows = toTaskGanttRows(
      [
        makeTask({ id: "late", title: "Late", startDate: "2026-08-09" }),
        makeTask({ id: "early", title: "Early", startDate: "2026-08-02" }),
      ],
      bounds,
      now,
    );
    expect(rows.map((r) => r.id)).toEqual(["early", "late"]);
  });

  it.each([
    { name: "open when due later", status: "open", dueOn: "2026-08-15", expected: "open" },
    { name: "overdue when due before today", status: "open", dueOn: "2026-08-04", expected: "overdue" },
    { name: "open on the day it is due", status: "open", dueOn: "2026-08-10", expected: "open" },
    { name: "closed regardless of due date", status: "closed", dueOn: "2026-08-04", expected: "closed" },
    { name: "open when it has no due date", status: "open", dueOn: null, expected: "open" },
  ])("marks a task $name", ({ status, dueOn, expected }) => {
    const [row] = toTaskGanttRows(
      [makeTask({ startDate: "2026-08-02", dueOn, status: status as Task["status"] })],
      bounds,
      now,
    );
    expect(row.state).toBe(expected);
  });
});

function makeBacklog(overrides: Partial<Backlog>): Backlog {
  return {
    id: "b1",
    projectId: "p1",
    name: "Backlog",
    description: "",
    startDate: null,
    dueOn: null,
    priority: "medium",
    progress: "not_started",
    defaultLinkedGitlabProjectId: null,
    baseBranch: "",
    allowedScope: "",
    assigneeUserId: null,
    assigneeUsername: "",
    assigneeDisplayName: "",
    forbiddenScope: "",
    taskCount: 0,
    closedTaskCount: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("backlogCompletion", () => {
  const tasks = [
    makeTask({ id: "t1", backlogId: "b1", status: "closed" }),
    makeTask({ id: "t2", backlogId: "b1", status: "open" }),
    makeTask({ id: "t3", backlogId: "b2", status: "closed" }),
    makeTask({ id: "t4", backlogId: null, status: "closed" }),
  ];

  it("counts only the tasks filed in that backlog", () => {
    expect(backlogCompletion(tasks, "b1")).toEqual({ closed: 1, total: 2, ratio: 0.5 });
  });

  // An empty backlog has not been finished, so it must not read as complete.
  it("reports 0/0 at ratio 0 for a backlog with no tasks", () => {
    expect(backlogCompletion(tasks, "b9")).toEqual({ closed: 0, total: 0, ratio: 0 });
  });

  it("reports a fully closed backlog at ratio 1", () => {
    expect(backlogCompletion(tasks, "b2").ratio).toBe(1);
  });
});

describe("backlogTaskCompletion", () => {
  it("reads the ratio off the backlog's own counts", () => {
    expect(backlogTaskCompletion({ taskCount: 2, closedTaskCount: 1 })).toEqual({
      closed: 1,
      total: 2,
      ratio: 0.5,
    });
  });

  // An empty backlog has not been finished, so it must not read as complete.
  it("reports 0/0 at ratio 0 for a backlog with no tasks", () => {
    expect(backlogTaskCompletion({ taskCount: 0, closedTaskCount: 0 })).toEqual({
      closed: 0,
      total: 0,
      ratio: 0,
    });
  });

  it("reports a fully closed backlog at ratio 1", () => {
    expect(backlogTaskCompletion({ taskCount: 2, closedTaskCount: 2 }).ratio).toBe(1);
  });
});

describe("toBacklogGanttRows", () => {
  const bounds = { start: startOfDay(new Date("2026-08-01")), end: startOfDay(new Date("2026-08-21")) };
  const now = new Date("2026-08-10T09:00:00Z");

  it("drops backlogs with no schedule", () => {
    expect(toBacklogGanttRows([makeBacklog({})], bounds, now)).toEqual([]);
  });

  it("carries the completion ratio onto the row", () => {
    const [row] = toBacklogGanttRows(
      [
        makeBacklog({
          id: "b1",
          startDate: "2026-08-03",
          dueOn: "2026-08-05",
          taskCount: 2,
          closedTaskCount: 1,
        }),
      ],
      bounds,
      now,
    );
    expect(row.completion).toEqual({ closed: 1, total: 2, ratio: 0.5 });
    expect(row.offset).toBe(2 * ONE_DAY_MS);
    expect(row.duration).toBe(3 * ONE_DAY_MS);
  });

  it("titles the row with the backlog's name", () => {
    const [row] = toBacklogGanttRows([makeBacklog({ name: "Sprint 1", dueOn: "2026-08-05" })], bounds, now);
    expect(row.title).toBe("Sprint 1");
  });

  it.each([
    {
      name: "closed once every task is closed",
      dueOn: "2026-08-04",
      taskCount: 2,
      closedTaskCount: 2,
      expected: "closed",
    },
    {
      name: "overdue while unfinished work sits past the due date",
      dueOn: "2026-08-04",
      taskCount: 2,
      closedTaskCount: 1,
      expected: "overdue",
    },
    {
      name: "open when the due date is still ahead",
      dueOn: "2026-08-15",
      taskCount: 1,
      closedTaskCount: 0,
      expected: "open",
    },
    // No tasks means nothing has been finished, overdue or not.
    { name: "overdue when empty and past due", dueOn: "2026-08-04", taskCount: 0, closedTaskCount: 0, expected: "overdue" },
    { name: "open when empty and not yet due", dueOn: "2026-08-15", taskCount: 0, closedTaskCount: 0, expected: "open" },
  ])("marks a backlog $name", ({ dueOn, taskCount, closedTaskCount, expected }) => {
    const [row] = toBacklogGanttRows(
      [makeBacklog({ id: "b1", startDate: "2026-08-02", dueOn, taskCount, closedTaskCount })],
      bounds,
      now,
    );
    expect(row.state).toBe(expected);
  });

  it("orders rows by start date", () => {
    const rows = toBacklogGanttRows(
      [
        makeBacklog({ id: "late", name: "Late", startDate: "2026-08-09" }),
        makeBacklog({ id: "early", name: "Early", startDate: "2026-08-02" }),
      ],
      bounds,
      now,
    );
    expect(rows.map((r) => r.id)).toEqual(["early", "late"]);
  });
});

describe("todayOffset", () => {
  const bounds = { start: startOfDay(new Date("2026-08-01")), end: startOfDay(new Date("2026-08-21")) };

  it("locates today within the range", () => {
    expect(todayOffset(bounds, new Date("2026-08-04T18:00:00Z"))).toBe(3 * ONE_DAY_MS);
  });

  it.each([
    { name: "before the range", now: "2026-07-20T00:00:00Z" },
    { name: "after the range", now: "2026-09-01T00:00:00Z" },
  ])("returns null when today is $name", ({ now }) => {
    expect(todayOffset(bounds, new Date(now))).toBeNull();
  });
});

describe("spanDays", () => {
  it("counts whole days across the range", () => {
    expect(
      spanDays({ start: startOfDay(new Date("2026-08-01")), end: startOfDay(new Date("2026-08-11")) }),
    ).toBe(10);
  });
});
