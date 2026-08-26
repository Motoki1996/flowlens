"use client";

import { useCallback, useEffect, useRef, useState, type Ref } from "react";
import Link from "next/link";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/** Long enough not to fire while the pointer crosses a row on its way
 *  somewhere else, short enough to read as part of the hover rather than as a
 *  wait. The browser's own `title` tooltip took about a second, which is long
 *  enough to give up on — which is why this is a real tooltip. */
const NAME_TOOLTIP_DELAY_MS = 150;

/** Written out rather than interpolated: Tailwind scans the source for whole
 *  class names, so a `line-clamp-${n}` would never be generated. */
const CLIP_CLASS = {
  1: "truncate",
  2: "line-clamp-2 break-words",
} as const;

/** A display utility would override the one `line-clamp-*` relies on — see the
 *  note on `className` below. `block` is what a caller reaches for out of
 *  habit; the rest are here because they would break the clamp just as
 *  quietly. */
const DISPLAY_CLASS = /^(inline-)?(block|flex|grid|table)$|^(inline|contents|flow-root)$/;

function stripDisplay(className?: string) {
  return className
    ?.split(" ")
    .filter((c) => !DISPLAY_CLASS.test(c))
    .join(" ");
}

/**
 * TruncatedName is an object's name wherever a collection view shows it: the
 * text clipped to the room the row or card actually has, and — only when it
 * really was clipped — the whole thing on hover or keyboard focus.
 *
 * Gating the tooltip on actual truncation is the point: one that repeats a
 * name already fully on screen is noise. A name is never allowed to grow the
 * layout instead, which is what a long title used to do to a board card.
 *
 * `lines` is how many lines the name gets before it clips: 1 for a list row,
 * where the name shares the line with badges and dates, 2 for a board card,
 * which has the height to spare and only the card's width to work with. Two
 * lines measures height rather than width, since that is what line-clamp
 * bounds.
 *
 * `className` must not carry a display utility. `line-clamp-2` works by
 * setting `display: -webkit-box`, and Tailwind emits `.block` *after*
 * `.line-clamp-*`, so a `block` passed in here silently wins and the name
 * wraps freely again — which is exactly the bug this component exists to fix.
 * It is dropped below rather than trusted, since nothing in jsdom can catch
 * it.
 *
 * Rendered as a <Link> when `href` is given, and otherwise as `as` — a <span>
 * in a row, an <h1> where the name is the screen's heading. A link stays
 * reachable by keyboard, so its tooltip is too; a heading is not focusable, so
 * there the tooltip is hover-only and the name is also in the edit form below
 * it.
 *
 * **Nothing from Radix is mounted until the name is first pointed at or
 * focused.** A project's task list is not paginated, so a screen can hold
 * thousands of these; a TooltipProvider and a Tooltip root per row would be
 * paid for on every one of them, to serve the handful a reader ever hovers.
 * Cold, this is the plain element plus two handlers. The open/close timing is
 * driven here rather than by the provider's own delay, so the first hover —
 * the one that arrives before Radix exists — behaves like every later one.
 */
export function TruncatedName({
  text,
  href,
  as: Tag = "span",
  lines = 1,
  className,
  tooltipText,
}: {
  text: string;
  href?: string;
  /** The element to render when there is no `href`. */
  as?: "span" | "h1";
  /** Lines the name gets before it clips. 1 (default) truncates, 2 clamps. */
  lines?: 1 | 2;
  className?: string;
  /** What the tooltip shows, when the full text is not what the row renders. */
  tooltipText?: string;
}) {
  const ref = useRef<HTMLElement>(null);
  // `mounted` is one-way: once a name has been hovered it keeps its tooltip
  // machinery, since a reader who hovered it once is likely to again.
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Wrapping the name in Radix replaces its DOM node, which drops focus when
  // the tooltip was reached by keyboard. Restoring it is the price of not
  // mounting Radix for every row.
  const refocus = useRef(false);

  const cancel = useCallback(() => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  }, []);

  useEffect(() => cancel, [cancel]);

  useEffect(() => {
    if (mounted && refocus.current) {
      refocus.current = false;
      ref.current?.focus();
    }
  }, [mounted]);

  const show = useCallback(
    (byKeyboard: boolean) => {
      cancel();
      timer.current = setTimeout(() => {
        // Measured at the moment of hover rather than tracked: a column can be
        // resized and the viewport is not, so anything cached would be stale.
        const el = ref.current;
        if (!el) return;
        const clipped =
          lines === 1 ? el.scrollWidth > el.clientWidth : el.scrollHeight > el.clientHeight;
        if (!clipped) return;
        refocus.current = byKeyboard && !mounted;
        setMounted(true);
        setOpen(true);
      }, NAME_TOOLTIP_DELAY_MS);
    },
    [cancel, lines, mounted],
  );

  const hide = useCallback(() => {
    cancel();
    setOpen(false);
  }, [cancel]);

  const classes = cn("min-w-0", CLIP_CLASS[lines], stripDisplay(className));
  const handlers = {
    onPointerEnter: () => show(false),
    onPointerLeave: hide,
    onFocus: () => show(true),
    onBlur: hide,
  };

  const name = href ? (
    <Link ref={ref as Ref<HTMLAnchorElement>} href={href} className={classes} {...handlers}>
      {text}
    </Link>
  ) : (
    <Tag ref={ref as Ref<HTMLSpanElement & HTMLHeadingElement>} className={classes} {...handlers}>
      {text}
    </Tag>
  );

  if (!mounted) return name;

  return (
    // The provider is per name rather than at the root because a section is
    // rendered on its own in tests and in Storybook, where no layout wraps it —
    // and Radix throws without an ancestor provider. Only hovered names pay for
    // it. Radix's own open attempts are ignored: `show` above owns that
    // decision, since it is the one that knows whether the name is clipped.
    <TooltipProvider delayDuration={NAME_TOOLTIP_DELAY_MS}>
      <Tooltip open={open} onOpenChange={(next) => next || hide()}>
        <TooltipTrigger asChild>{name}</TooltipTrigger>
        <TooltipContent side="top" align="start" className="max-w-xs">
          {tooltipText ?? text}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
