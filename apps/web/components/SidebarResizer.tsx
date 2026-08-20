"use client";

import * as React from "react";
import { useSidebar } from "@/components/ui/sidebar";
import {
  SIDEBAR_COOKIE_MAX_AGE,
  SIDEBAR_WIDTH_COOKIE,
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_MAX,
  SIDEBAR_WIDTH_MIN,
  SIDEBAR_WIDTH_STEP,
  clampSidebarWidth,
} from "@/lib/sidebar";
import { cn } from "@/lib/utils";

/** The element SidebarProvider hangs `--sidebar-width` off. */
function wrapperOf(node: HTMLElement | null): HTMLElement | null {
  return node?.closest<HTMLElement>('[data-slot="sidebar-wrapper"]') ?? null;
}

function readWidth(wrapper: HTMLElement): number {
  // Inline style first: it is where both SidebarProvider and this component
  // write the variable, and it is the one value jsdom can read back.
  const raw =
    wrapper.style.getPropertyValue("--sidebar-width") ||
    getComputedStyle(wrapper).getPropertyValue("--sidebar-width");
  return clampSidebarWidth(parseFloat(raw) || SIDEBAR_WIDTH_DEFAULT);
}

/**
 * SidebarResizer is the drag handle shadcn's sidebar doesn't ship: its width
 * is a CSS variable, so making it adjustable is a matter of writing that
 * variable and remembering the result.
 *
 * The drag writes `--sidebar-width` straight onto the DOM rather than through
 * React state — a pointermove fires far more often than a render is worth, and
 * the value's real home is the cookie the server reads on the next request.
 * It is a `separator` with `aria-valuenow`, so the same adjustment is
 * available from the keyboard, which a pointer-only handle would deny.
 *
 * Rendered inside <Sidebar>, where it positions itself against the fixed
 * sidebar container. It is absent while collapsed and on mobile (a drawer),
 * neither of which has a width to adjust.
 */
export function SidebarResizer({ className }: { className?: string }) {
  const { state, isMobile } = useSidebar();
  const ref = React.useRef<HTMLDivElement>(null);
  const drag = React.useRef<{ startX: number; startWidth: number } | null>(null);
  // Mirrors the DOM only for assistive technology; the drag itself never reads it.
  const [width, setWidth] = React.useState(SIDEBAR_WIDTH_DEFAULT);

  React.useEffect(() => {
    const wrapper = wrapperOf(ref.current);
    if (wrapper) setWidth(readWidth(wrapper));
  }, []);

  const apply = React.useCallback((next: number, persist: boolean) => {
    const wrapper = wrapperOf(ref.current);
    if (!wrapper) return;
    const clamped = clampSidebarWidth(next);
    wrapper.style.setProperty("--sidebar-width", `${clamped}px`);
    setWidth(clamped);
    if (persist) {
      document.cookie = `${SIDEBAR_WIDTH_COOKIE}=${clamped}; path=/; max-age=${SIDEBAR_COOKIE_MAX_AGE}; samesite=lax`;
    }
  }, []);

  function onPointerDown(event: React.PointerEvent<HTMLDivElement>) {
    // Left button only: a right-click must not start a drag that never ends.
    if (event.button !== 0) return;
    const wrapper = wrapperOf(ref.current);
    if (!wrapper) return;
    drag.current = { startX: event.clientX, startWidth: readWidth(wrapper) };
    // Suppresses the width transition and text selection for the duration.
    wrapper.dataset.resizing = "true";
    event.currentTarget.setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function onPointerMove(event: React.PointerEvent<HTMLDivElement>) {
    if (!drag.current) return;
    apply(drag.current.startWidth + (event.clientX - drag.current.startX), false);
  }

  function endDrag(event: React.PointerEvent<HTMLDivElement>) {
    if (!drag.current) return;
    drag.current = null;
    const wrapper = wrapperOf(ref.current);
    if (wrapper) {
      delete wrapper.dataset.resizing;
      apply(readWidth(wrapper), true);
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    const step =
      event.key === "ArrowLeft" ? -SIDEBAR_WIDTH_STEP : event.key === "ArrowRight" ? SIDEBAR_WIDTH_STEP : 0;
    if (step !== 0) {
      event.preventDefault();
      apply(width + step, true);
      return;
    }
    if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      apply(event.key === "Home" ? SIDEBAR_WIDTH_MIN : SIDEBAR_WIDTH_MAX, true);
    }
  }

  // Nothing to resize: the icon rail has a fixed width, and on mobile the
  // sidebar is a sheet.
  if (isMobile || state === "collapsed") return null;

  return (
    <div
      ref={ref}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      aria-valuenow={width}
      aria-valuemin={SIDEBAR_WIDTH_MIN}
      aria-valuemax={SIDEBAR_WIDTH_MAX}
      tabIndex={0}
      title="Drag to resize, double-click to reset"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onDoubleClick={() => apply(SIDEBAR_WIDTH_DEFAULT, true)}
      onKeyDown={onKeyDown}
      className={cn(
        "absolute inset-y-0 -right-2 z-20 hidden w-4 cursor-col-resize touch-none md:block",
        "after:bg-sidebar-border/0 hover:after:bg-sidebar-border after:absolute after:inset-y-0 after:left-1/2 after:w-0.5 after:-translate-x-1/2 after:transition-colors",
        "focus-visible:after:bg-ring focus-visible:outline-none",
        className,
      )}
    />
  );
}
