import { describe, it, expect } from "vitest";
import { classifyDueTasks, dueStatus, endOfWeek } from "./dashboard";
import type { TaskWithProject } from "@/types";

function makeTask(overrides: Partial<TaskWithProject>): TaskWithProject {
  return {
    id: "t1",
    projectId: "p1",
    projectName: "Project",
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
    priority: "medium",
    progress: "not_started",
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

describe("endOfWeek", () => {
  it("returns the upcoming Sunday for a Wednesday", () => {
    // 2026-08-05 is a Wednesday.
    const result = endOfWeek(new Date(2026, 7, 5));
    expect(result).toEqual(new Date(2026, 7, 9));
  });

  it("returns the same day for a Sunday", () => {
    // 2026-08-09 is a Sunday.
    const result = endOfWeek(new Date(2026, 7, 9));
    expect(result).toEqual(new Date(2026, 7, 9));
  });

  it("returns the same day for a Monday plus 6 days", () => {
    // 2026-08-03 is a Monday.
    const result = endOfWeek(new Date(2026, 7, 3));
    expect(result).toEqual(new Date(2026, 7, 9));
  });
});

describe("classifyDueTasks", () => {
  // "Now" is Wednesday 2026-08-05; the week runs through Sunday 2026-08-09.
  const now = new Date(2026, 7, 5, 12, 0, 0);

  it("puts a task due before today in overdue", () => {
    const task = makeTask({ id: "overdue", dueOn: "2026-08-04" });
    const { overdue, dueSoon } = classifyDueTasks([task], now);
    expect(overdue.map((t) => t.id)).toEqual(["overdue"]);
    expect(dueSoon).toEqual([]);
  });

  it("puts a task due today in dueSoon", () => {
    const task = makeTask({ id: "today", dueOn: "2026-08-05" });
    const { overdue, dueSoon } = classifyDueTasks([task], now);
    expect(overdue).toEqual([]);
    expect(dueSoon.map((t) => t.id)).toEqual(["today"]);
  });

  it("puts a task due at the end of this week in dueSoon", () => {
    const task = makeTask({ id: "sunday", dueOn: "2026-08-09" });
    const { dueSoon } = classifyDueTasks([task], now);
    expect(dueSoon.map((t) => t.id)).toEqual(["sunday"]);
  });

  it("excludes a task due after this week", () => {
    const task = makeTask({ id: "next-week", dueOn: "2026-08-10" });
    const { overdue, dueSoon } = classifyDueTasks([task], now);
    expect(overdue).toEqual([]);
    expect(dueSoon).toEqual([]);
  });

  it("excludes a task with no due date from both buckets", () => {
    const task = makeTask({ id: "undated", dueOn: null });
    const { overdue, dueSoon } = classifyDueTasks([task], now);
    expect(overdue).toEqual([]);
    expect(dueSoon).toEqual([]);
  });
});

describe("dueStatus", () => {
  // "Now" is Wednesday 2026-08-05; the week runs through Sunday 2026-08-09,
  // the same fixture classifyDueTasks uses above (issue #148).
  const now = new Date(2026, 7, 5, 12, 0, 0);

  it("returns undated for no due date", () => {
    expect(dueStatus(null, now)).toBe("undated");
  });

  it("returns overdue for yesterday", () => {
    expect(dueStatus("2026-08-04", now)).toBe("overdue");
  });

  it("returns dueSoon for today", () => {
    expect(dueStatus("2026-08-05", now)).toBe("dueSoon");
  });

  it("returns dueSoon for the last day of this week", () => {
    expect(dueStatus("2026-08-09", now)).toBe("dueSoon");
  });

  it("returns later for a date after this week", () => {
    expect(dueStatus("2026-08-10", now)).toBe("later");
  });
});
