import { describe, it, expect } from "vitest";
import {
  computeAxis,
  computeTimelineBounds,
  effectiveRange,
  formatAxisTick,
  hasSchedule,
  spanDays,
  startOfDay,
  toGanttRows,
  todayOffset,
} from "./timeline";
import type { Task } from "@/types";

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

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
    title: "Task",
    description: "",
    status: "open",
    closedAt: null,
    assigneeGitlabUserId: null,
    assigneeGitlabUsername: "",
    labels: [],
    dueOn: null,
    startDate: null,
    position: 0,
    createdByUserId: "u1",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    gitlab: null,
    aiContext: {
      acceptanceCriteria: "",
      aiContext: "",
      allowedScope: "",
      forbiddenScope: "",
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
  const boundsOf = (startDay: string, days: number) => ({
    start: startOfDay(new Date(startDay)),
    end: new Date(startOfDay(new Date(startDay)).getTime() + days * ONE_DAY_MS),
  });

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

  it("keeps every tick inside the plotted range", () => {
    const bounds = boundsOf("2026-08-03", 40);
    const total = bounds.end.getTime() - bounds.start.getTime();
    for (const tick of computeAxis(bounds).ticks) {
      expect(tick).toBeGreaterThanOrEqual(0);
      expect(tick).toBeLessThan(total);
    }
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

describe("toGanttRows", () => {
  const bounds = { start: startOfDay(new Date("2026-08-01")), end: startOfDay(new Date("2026-08-21")) };
  const now = new Date("2026-08-10T09:00:00Z");

  it("drops tasks with no schedule", () => {
    const rows = toGanttRows([makeTask({ id: "t1" })], bounds, now);
    expect(rows).toEqual([]);
  });

  it("offsets a bar from the range start and spans whole days inclusively", () => {
    const [row] = toGanttRows(
      [makeTask({ startDate: "2026-08-03", dueOn: "2026-08-05" })],
      bounds,
      now,
    );
    expect(row.offset).toBe(2 * ONE_DAY_MS);
    // 08-03 through 08-05 inclusive is three days wide, not two.
    expect(row.duration).toBe(3 * ONE_DAY_MS);
  });

  it("gives a single-day task a full day of width", () => {
    const [row] = toGanttRows([makeTask({ dueOn: "2026-08-05" })], bounds, now);
    expect(row.duration).toBe(ONE_DAY_MS);
  });

  it("orders rows by start date", () => {
    const rows = toGanttRows(
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
    const [row] = toGanttRows(
      [makeTask({ startDate: "2026-08-02", dueOn, status: status as Task["status"] })],
      bounds,
      now,
    );
    expect(row.state).toBe(expected);
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
