/**
 * Shared constants for the project sidebar's persisted geometry.
 *
 * Both halves of it survive a reload through a cookie rather than
 * localStorage, because the server layout has to know the width and the
 * open/closed state *before* it renders: reading them on the client would
 * mean one frame at the default width and a visible jump. Open/closed uses
 * shadcn's own cookie name (components/ui/sidebar.tsx writes it); the width is
 * ours.
 */

/** Written by shadcn's SidebarProvider itself — do not rename. */
export const SIDEBAR_STATE_COOKIE = "sidebar_state";
export const SIDEBAR_WIDTH_COOKIE = "flowlens_sidebar_width";
export const SIDEBAR_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export const SIDEBAR_WIDTH_DEFAULT = 240;
/** Narrow enough to be worth doing, still wide enough for "Merge requests". */
export const SIDEBAR_WIDTH_MIN = 180;
export const SIDEBAR_WIDTH_MAX = 480;
/** One nudge of an arrow key. */
export const SIDEBAR_WIDTH_STEP = 16;

export function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_WIDTH_MAX, Math.max(SIDEBAR_WIDTH_MIN, Math.round(width)));
}

/**
 * Reads the width cookie, falling back to the default for anything absent or
 * unparseable — a hand-edited cookie must not be able to render the sidebar
 * unusable.
 */
export function sidebarWidthFromCookie(value: string | undefined): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? clampSidebarWidth(parsed) : SIDEBAR_WIDTH_DEFAULT;
}

/** The sidebar is open unless the cookie says otherwise (first visit included). */
export function sidebarOpenFromCookie(value: string | undefined): boolean {
  return value !== "false";
}
