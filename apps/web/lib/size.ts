import type { Size } from "@/types";

/**
 * The five sizes, smallest first — the axis reads left to right as growing
 * work. Shared by the size filter menu, the edit form's select and the badge
 * so the order never disagrees between them.
 */
export const SIZE_OPTIONS: { size: Size; label: string }[] = [
  { size: "xs", label: "XS" },
  { size: "s", label: "S" },
  { size: "m", label: "M" },
  { size: "l", label: "L" },
  { size: "xl", label: "XL" },
];

/** SIZE_LABELS is SIZE_OPTIONS keyed by value, for the badge, the filter
 *  menus and anywhere a single size has to be rendered on its own. */
export const SIZE_LABELS: Record<Size, string> = {
  xs: "XS",
  s: "S",
  m: "M",
  l: "L",
  xl: "XL",
};

/**
 * SIZE_POINTS mirrors apps/api/internal/velocity's sizePoints weight table.
 * The server is the source of truth — every points figure the UI displays
 * comes from the velocity API already weighted, never from multiplying here.
 * This copy exists only to *explain* the weighting to a reader (the size
 * select shows "L (5 pts)"), so it must be kept in step with the Go table
 * but is never used to compute a displayed metric.
 */
export const SIZE_POINTS: Record<Size, number> = {
  xs: 1,
  s: 2,
  m: 3,
  l: 5,
  xl: 8,
};

/** DEFAULT_SIZE is the value a task gets when nobody picks one — the exact
 *  middle of the five. Callers use it to tell "deliberately medium" apart
 *  from "never sized", which is what sizedTaskRatio measures in aggregate. */
export const DEFAULT_SIZE: Size = "m";
