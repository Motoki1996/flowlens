import { describe, it, expect } from "vitest";
import { barStyle, computeTimelineBounds, effectiveRange, hasSchedule } from "./timeline";

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
    expect(r?.start.toISOString().slice(0, 10)).toBe("2026-08-01");
    expect(r?.end.toISOString().slice(0, 10)).toBe("2026-08-05");
  });

  it("swaps a dueOn set before startDate so the range is never inverted", () => {
    const r = effectiveRange({ startDate: "2026-08-05", dueOn: "2026-08-01" });
    expect(r?.start.toISOString().slice(0, 10)).toBe("2026-08-01");
    expect(r?.end.toISOString().slice(0, 10)).toBe("2026-08-05");
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

  it("spans the earliest start to the latest end, padded by one day", () => {
    const bounds = computeTimelineBounds([
      { startDate: "2026-08-01", dueOn: "2026-08-03" },
      { startDate: "2026-08-10", dueOn: null },
    ]);
    expect(bounds?.start.toISOString().slice(0, 10)).toBe("2026-07-31");
    expect(bounds?.end.toISOString().slice(0, 10)).toBe("2026-08-11");
  });

  it("ignores unscheduled tasks mixed in with scheduled ones", () => {
    const bounds = computeTimelineBounds([
      { startDate: null, dueOn: null },
      { startDate: "2026-08-01", dueOn: "2026-08-01" },
    ]);
    expect(bounds?.start.toISOString().slice(0, 10)).toBe("2026-07-31");
    expect(bounds?.end.toISOString().slice(0, 10)).toBe("2026-08-02");
  });
});

describe("barStyle", () => {
  const bounds = { start: new Date("2026-08-01T00:00:00Z"), end: new Date("2026-08-11T00:00:00Z") };

  it("returns null for a task with no schedule", () => {
    expect(barStyle({ startDate: null, dueOn: null }, bounds)).toBeNull();
  });

  it("positions a bar proportionally within bounds", () => {
    const style = barStyle({ startDate: "2026-08-03", dueOn: "2026-08-05" }, bounds);
    expect(style?.leftPercent).toBeCloseTo(20, 5);
    expect(style?.widthPercent).toBeCloseTo(20, 5);
  });

  it("gives a single-day task a minimum visible width", () => {
    const style = barStyle({ startDate: "2026-08-05", dueOn: "2026-08-05" }, bounds);
    expect(style?.widthPercent).toBeGreaterThanOrEqual(1.5);
  });
});
