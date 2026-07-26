import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Project } from "@/types";
import { ProjectsView } from "./ProjectsView";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));

const project: Project = {
  id: "1",
  name: "Alpha",
  description: "The first project",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

describe("ProjectsView", () => {
  it("renders the empty state when there are no projects", () => {
    render(<ProjectsView projects={[]} />);
    expect(screen.getByText("No projects yet")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("renders a card per project, linking to its single view", () => {
    render(<ProjectsView projects={[project]} />);
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByRole("link")).toHaveAttribute("href", "/projects/1");
  });

  it("renders an error state instead of the empty state when the fetch failed", () => {
    render(<ProjectsView projects={[]} error />);
    expect(screen.getByText(/Failed to load projects/i)).toBeInTheDocument();
    expect(screen.queryByText("No projects yet")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New project" })).not.toBeInTheDocument();
  });
});
