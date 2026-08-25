"use client";

import { SidebarTrigger, useSidebar } from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";

/**
 * SidebarToggle is the ProjectSidebar's open/close control.
 *
 * shadcn's own SidebarTrigger is a 28px ghost icon with the fixed label
 * "Toggle Sidebar": on the header's card background it reads as decoration
 * rather than as a button, and the label never says which way it will go.
 * This wraps it with the affordance the rest of the app's buttons have — a
 * larger hit area, a foreground colour that lifts on hover — and names itself
 * after the action it will actually perform.
 *
 * Its home is the sidebar's own header (`placement="sidebar"`), where it stays
 * visible in the icon rail so the collapsed sidebar carries its own way back
 * out. `placement="header"` is the app header's copy, and exists only for
 * mobile — there the sidebar is a drawer, and while that drawer is closed the
 * control inside it cannot be reached — so it hides itself from md up rather
 * than sitting in the top bar beside a sidebar that already has one.
 *
 * Must be rendered inside a SidebarProvider.
 */
export function SidebarToggle({
  placement = "header",
  className,
}: {
  placement?: "header" | "sidebar";
  className?: string;
}) {
  const { state, isMobile } = useSidebar();
  // On mobile the sidebar is a sheet, which is never the "collapsed" rail:
  // the header's control always opens it.
  const expanded = isMobile || state === "expanded";
  const label = expanded ? "Collapse sidebar" : "Expand sidebar";

  return (
    <SidebarTrigger
      // SidebarTrigger renders its own icon and a fixed "Toggle Sidebar"
      // caption; aria-label is what overrides that caption from out here.
      aria-label={label}
      title={label}
      className={cn(
        "text-muted-foreground hover:text-foreground size-8",
        placement === "header" && "-ml-1 md:hidden",
        // In the icon rail the switcher beside it is gone, so the toggle takes
        // the header's full width and centres itself over the menu icons.
        placement === "sidebar" && "group-data-[collapsible=icon]:w-full",
        className,
      )}
    />
  );
}
