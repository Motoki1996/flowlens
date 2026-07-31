import { describe, it, expect, vi, beforeEach } from "vitest";

const getCurrentUser = vi.fn();
const getTask = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getTask: (id: string) => getTask(id),
}));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import LegacyTaskPage from "./page";

describe("LegacyTaskPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue({ id: "1", username: "octocat", email: "", displayName: "" });
  });

  it("forwards to the task's nested route", async () => {
    getTask.mockResolvedValue({ id: "t1", projectId: "p1" });
    await expect(LegacyTaskPage({ params: Promise.resolve({ taskId: "t1" }) })).rejects.toThrow(
      "REDIRECT:/projects/p1/tasks/t1",
    );
  });

  it("renders not-found when the task doesn't exist", async () => {
    getTask.mockResolvedValue(null);
    await expect(
      LegacyTaskPage({ params: Promise.resolve({ taskId: "unknown" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });
});
