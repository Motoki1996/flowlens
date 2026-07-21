import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { User } from "@/types";

const user: User = {
  id: "1",
  githubUserId: 7,
  githubLogin: "octocat",
  displayName: "The Octocat",
  avatarUrl: "",
};

const getCurrentUser = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  redirect: (url: string) => {
    throw new Error(`REDIRECT:${url}`);
  },
}));

import DashboardPage from "./page";

describe("DashboardPage", () => {
  it("renders the empty state for an authenticated user", async () => {
    getCurrentUser.mockResolvedValue(user);
    render(await DashboardPage());
    expect(
      screen.getByRole("heading", { name: "Dashboard" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/No repositories connected yet/i),
    ).toBeInTheDocument();
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(DashboardPage()).rejects.toThrow("REDIRECT:/login");
  });
});
