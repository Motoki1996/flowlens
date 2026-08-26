import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Breadcrumbs } from "./Breadcrumbs";

const LONG = "Reconcile the outbox worker with the webhook receiver";

describe("Breadcrumbs", () => {
  it("links every ancestor and marks the last crumb as the current page", () => {
    render(
      <Breadcrumbs
        items={[
          { label: "Projects", href: "/projects" },
          { label: "FlowLens", href: "/projects/p1" },
          { label: "Sprint 1" },
        ]}
      />,
    );

    expect(screen.getByRole("link", { name: "Projects" })).toHaveAttribute("href", "/projects");
    expect(screen.getByRole("link", { name: "FlowLens" })).toHaveAttribute("href", "/projects/p1");
    expect(screen.queryByRole("link", { name: "Sprint 1" })).toBeNull();
    expect(screen.getByText("Sprint 1")).toHaveAttribute("aria-current", "page");
  });

  // A crumb only points at another screen, so it takes a share of the line and
  // no more — an object's name can be a paragraph long, and the trail used to
  // wrap until it pushed the heading below the fold. The clip itself is CSS,
  // so the cap is what a test can see.
  it("caps every crumb's width, current page included", () => {
    render(<Breadcrumbs items={[{ label: LONG, href: "/projects/p1" }, { label: LONG }]} />);

    for (const crumb of screen.getAllByText(LONG)) {
      expect(crumb).toHaveClass("max-w-[10rem]", "sm:max-w-[16rem]");
    }
  });

  it("offers an ancestor's full label on hover once it has been clipped", async () => {
    const user = userEvent.setup();
    render(<Breadcrumbs items={[{ label: LONG, href: "/projects/p1" }, { label: "Sprint 1" }]} />);

    const crumb = screen.getByRole("link", { name: LONG });
    Object.defineProperty(crumb, "scrollWidth", { value: 400, configurable: true });
    Object.defineProperty(crumb, "clientWidth", { value: 160, configurable: true });
    await user.hover(crumb);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(LONG);
  });
});
