import type { MouseEvent } from "react";

/**
 * isCardBackgroundClick tells a board card whether a click landed on the card
 * itself rather than on a control it contains — the whole card navigates to the
 * object's single view, but the title link, the "View tasks" link and the
 * progress select each keep doing their own job.
 *
 * A text selection is not a click-through either: dragging across a card's
 * title to copy it should not navigate away from the board.
 */
export function isCardBackgroundClick(e: MouseEvent<HTMLElement>): boolean {
  if (e.defaultPrevented || e.button !== 0) return false;
  if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return false;
  if (typeof window !== "undefined" && (window.getSelection()?.toString() ?? "") !== "") {
    return false;
  }
  const target = e.target as HTMLElement | null;
  return !target?.closest('a, button, input, select, textarea, label, [role="combobox"]');
}
