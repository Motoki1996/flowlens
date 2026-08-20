import { describe, it, expect, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Sidebar, SidebarProvider } from "@/components/ui/sidebar";
import { SidebarResizer } from "./SidebarResizer";
import { SIDEBAR_WIDTH_MAX, SIDEBAR_WIDTH_MIN } from "@/lib/sidebar";

function renderResizer({ open = true }: { open?: boolean } = {}) {
  const { container } = render(
    <SidebarProvider
      defaultOpen={open}
      style={{ "--sidebar-width": "240px" } as React.CSSProperties}
    >
      <Sidebar collapsible="icon">
        <SidebarResizer />
      </Sidebar>
    </SidebarProvider>,
  );
  return container.querySelector<HTMLElement>('[data-slot="sidebar-wrapper"]')!;
}

function width(wrapper: HTMLElement) {
  return wrapper.style.getPropertyValue("--sidebar-width");
}

describe("SidebarResizer", () => {
  beforeEach(() => {
    document.cookie = "flowlens_sidebar_width=; path=/; max-age=0";
  });

  // A pointer-only handle would put the width out of reach of anyone not using
  // one, so the same adjustment is on the arrow keys.
  it("widens and narrows the sidebar from the keyboard", async () => {
    const wrapper = renderResizer();
    const handle = screen.getByRole("separator", { name: "Resize sidebar" });

    handle.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(width(wrapper)).toBe("256px");
    await userEvent.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(width(wrapper)).toBe("224px");
    expect(handle).toHaveAttribute("aria-valuenow", "224");
  });

  it("stops at the minimum and maximum rather than letting the sidebar vanish", async () => {
    const wrapper = renderResizer();
    screen.getByRole("separator", { name: "Resize sidebar" }).focus();

    await userEvent.keyboard("{Home}");
    expect(width(wrapper)).toBe(`${SIDEBAR_WIDTH_MIN}px`);
    await userEvent.keyboard("{ArrowLeft}");
    expect(width(wrapper)).toBe(`${SIDEBAR_WIDTH_MIN}px`);

    await userEvent.keyboard("{End}");
    expect(width(wrapper)).toBe(`${SIDEBAR_WIDTH_MAX}px`);
    await userEvent.keyboard("{ArrowRight}");
    expect(width(wrapper)).toBe(`${SIDEBAR_WIDTH_MAX}px`);
  });

  // The cookie is the real store: the server reads it on the next request.
  it("remembers the new width in a cookie", async () => {
    renderResizer();
    screen.getByRole("separator", { name: "Resize sidebar" }).focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(document.cookie).toContain("flowlens_sidebar_width=256");
  });

  it("double-click restores the default width", async () => {
    const wrapper = renderResizer();
    const handle = screen.getByRole("separator", { name: "Resize sidebar" });
    handle.focus();
    await userEvent.keyboard("{End}");
    await userEvent.dblClick(handle);
    expect(width(wrapper)).toBe("240px");
  });

  it("is absent while the sidebar is collapsed, which has no width to adjust", () => {
    renderResizer({ open: false });
    expect(screen.queryByRole("separator", { name: "Resize sidebar" })).not.toBeInTheDocument();
  });
});
