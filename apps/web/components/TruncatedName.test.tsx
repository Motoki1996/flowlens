import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TruncatedName } from "./TruncatedName";

// jsdom lays nothing out, so the two measurements that decide whether a name
// was clipped are set on the element directly — the same trick the timeline's
// own tooltip test uses.
function measure(el: HTMLElement, sizes: Record<string, number>) {
  for (const [prop, value] of Object.entries(sizes)) {
    Object.defineProperty(el, prop, { value, configurable: true });
  }
}

const LONG = "Reconcile the outbox worker with the webhook receiver";

describe("TruncatedName", () => {
  // The tooltip exists to recover text the layout took away, so the axis that
  // matters is clipped/not — measured by width on one line and by height once
  // the name is clamped to several.
  const cases = [
    {
      name: "one line, clipped",
      lines: 1 as const,
      sizes: {
        scrollWidth: 400,
        clientWidth: 200,
        scrollHeight: 20,
        clientHeight: 20,
      },
      tooltip: true,
    },
    {
      name: "one line, fits",
      lines: 1 as const,
      sizes: {
        scrollWidth: 120,
        clientWidth: 200,
        scrollHeight: 20,
        clientHeight: 20,
      },
      tooltip: false,
    },
    {
      name: "two lines, clamped",
      lines: 2 as const,
      sizes: {
        scrollWidth: 200,
        clientWidth: 200,
        scrollHeight: 60,
        clientHeight: 40,
      },
      tooltip: true,
    },
    {
      name: "two lines, fits",
      lines: 2 as const,
      sizes: {
        scrollWidth: 200,
        clientWidth: 200,
        scrollHeight: 40,
        clientHeight: 40,
      },
      tooltip: false,
    },
  ];

  for (const c of cases) {
    it(`${c.tooltip ? "offers" : "withholds"} the full name — ${c.name}`, async () => {
      const user = userEvent.setup();
      render(<TruncatedName text={LONG} lines={c.lines} />);

      const el = screen.getByText(LONG);
      measure(el, c.sizes);
      await user.hover(el);

      if (c.tooltip) {
        expect(await screen.findByRole("tooltip")).toHaveTextContent(LONG);
      } else {
        // Well past the hover delay, so this is "never opened" rather than
        // "not yet".
        await expect(screen.findByRole("tooltip", {}, { timeout: 400 })).rejects.toThrow();
      }
    });
  }

  // The whole reason the tooltip is mounted lazily: a project's task list is
  // not paginated, so a screen can hold thousands of names and only a handful
  // are ever hovered.
  it("mounts nothing from the tooltip library until the name is pointed at", async () => {
    const user = userEvent.setup();
    render(<TruncatedName text={LONG} />);

    const el = screen.getByText(LONG);
    expect(el).not.toHaveAttribute("data-slot", "tooltip-trigger");

    measure(el, { scrollWidth: 400, clientWidth: 200 });
    await user.hover(el);
    await screen.findByRole("tooltip");
    expect(screen.getByText(LONG, { selector: "[data-slot='tooltip-trigger']" })).toBeInTheDocument();
  });

  // Wrapping the name in the tooltip replaces its DOM node, so a name reached
  // by keyboard would otherwise lose focus at the moment it explained itself.
  it("keeps focus on a name that opened its tooltip from the keyboard", async () => {
    const user = userEvent.setup();
    render(<TruncatedName text={LONG} href="/projects/p1" />);

    measure(screen.getByText(LONG), { scrollWidth: 400, clientWidth: 200 });
    await user.tab();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(LONG);
    expect(screen.getByRole("link", { name: LONG })).toHaveFocus();
  });

  it("links to the object when given an href, and is a plain span otherwise", () => {
    const { rerender } = render(<TruncatedName text="Sprint 1" href="/projects/p1" />);
    expect(screen.getByRole("link", { name: "Sprint 1" })).toHaveAttribute("href", "/projects/p1");

    rerender(<TruncatedName text="Sprint 1" />);
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Sprint 1")).toBeInTheDocument();
  });
});
