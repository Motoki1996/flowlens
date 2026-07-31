import { describe, it, expect, vi, beforeEach } from "vitest";

const getCurrentUser = vi.fn();
const getBacklog = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
  getBacklog: (id: string) => getBacklog(id),
}));
vi.mock("next/navigation", () => ({
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
  notFound: () => {
    throw new Error("NOT_FOUND");
  },
}));

import LegacyBacklogPage from "./page";

describe("LegacyBacklogPage", () => {
  beforeEach(() => {
    getCurrentUser.mockResolvedValue({ id: "1", username: "octocat", email: "", displayName: "" });
  });

  it("forwards to the backlog's nested route", async () => {
    getBacklog.mockResolvedValue({ id: "b1", projectId: "p1" });
    await expect(
      LegacyBacklogPage({ params: Promise.resolve({ backlogId: "b1" }) }),
    ).rejects.toThrow("REDIRECT:/projects/p1/backlogs/b1");
  });

  it("renders not-found when the backlog doesn't exist", async () => {
    getBacklog.mockResolvedValue(null);
    await expect(
      LegacyBacklogPage({ params: Promise.resolve({ backlogId: "unknown" }) }),
    ).rejects.toThrow("NOT_FOUND");
  });
});
