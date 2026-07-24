import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { User } from "@/types";

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
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
      screen.getByText(/No GitLab projects connected yet/i),
    ).toBeInTheDocument();
  });

  it("redirects to /login when not authenticated", async () => {
    getCurrentUser.mockResolvedValue(null);
    await expect(DashboardPage()).rejects.toThrow("REDIRECT:/login");
  });
});
