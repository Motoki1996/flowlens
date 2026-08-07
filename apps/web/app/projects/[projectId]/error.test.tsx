import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ProjectError from "./error";

vi.mock("next/navigation", () => ({
  useParams: () => ({ projectId: "p1" }),
}));

describe("app/projects/[projectId]/error", () => {
  it("keeps the project sidebar and offers a retry", async () => {
    const reset = vi.fn();
    render(<ProjectError error={new Error("boom")} reset={reset} />);

    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Tasks" })).toHaveAttribute(
      "href",
      "/projects/p1/tasks",
    );
    expect(screen.getByRole("link", { name: "Backlogs" })).toHaveAttribute(
      "href",
      "/projects/p1/backlogs",
    );
    expect(screen.getByRole("link", { name: "All projects" })).toHaveAttribute("href", "/projects");

    await userEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
