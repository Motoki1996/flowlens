import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { AppHeader } from "./AppHeader";
import type { User } from "@/types";

// LogoutButton uses next/navigation; provide a stub router.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

const user: User = {
  id: "1",
  username: "octocat",
  email: "octocat@example.com",
  displayName: "The Octocat",
};

describe("AppHeader", () => {
  it("shows the display name and navigation", () => {
    render(<AppHeader user={user} />);
    expect(screen.getByText("The Octocat")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Dashboard" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Tasks" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign out/i })).toBeInTheDocument();
  });
});
