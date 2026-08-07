import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { User } from "@/types";

const getCurrentUser = vi.fn();

vi.mock("@/lib/api", () => ({
  getCurrentUser: () => getCurrentUser(),
}));

// AppHeader's LogoutButton uses next/navigation.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

describe("app/not-found", () => {
  it("shows the real header and a dashboard link when signed in", async () => {
    getCurrentUser.mockResolvedValue(user);
    const { default: NotFound } = await import("./not-found");
    render(await NotFound());

    expect(screen.getByText("Page not found")).toBeInTheDocument();
    expect(screen.getByText("The Octocat")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /go to dashboard/i })).toHaveAttribute(
      "href",
      "/dashboard",
    );
  });

  it("falls back to the static header and a login link when signed out", async () => {
    getCurrentUser.mockResolvedValue(null);
    const { default: NotFound } = await import("./not-found");
    render(await NotFound());

    expect(screen.getByText("Page not found")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /go to login/i })).toHaveAttribute("href", "/login");
  });
});
