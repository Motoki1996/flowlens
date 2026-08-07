"use client";

import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  defaultZoom,
  plotWidth as plotWidthFor,
  todayOffset,
  type DateRange,
  type TimelineZoom,
} from "@/lib/timeline";

/**
 * useTimelineViewport owns the two things a reader controls on a Gantt chart —
 * how much detail a day gets, and which part of the range is on screen — so the
 * Task and the Backlog timeline behave identically without duplicating the
 * logic.
 *
 * Zoom is view state, not a property of the collection: it is held locally,
 * exactly like the view-mode toggle it sits beside, and is not persisted to the
 * URL or the API. The plotted *range* stays derived from the data, so zooming
 * never hides a scheduled object — it only changes how far you scroll to reach it.
 *
 * `bounds` is nullable because a section computes it before it knows whether
 * there is anything to plot, and hooks cannot be called conditionally.
 */
export function useTimelineViewport(bounds: DateRange | null, now: Date) {
  // null means "follow the data": until the reader picks a level, the zoom
  // tracks whatever span the collection currently has.
  const [chosenZoom, setZoom] = useState<TimelineZoom | null>(null);
  const zoom = chosenZoom ?? (bounds ? defaultZoom(bounds) : "week");
  const width = bounds ? plotWidthFor(bounds, zoom) : 0;

  /**
   * todayRatio is where "now" sits as a fraction of the plot, which survives a
   * zoom change — pixel offsets do not.
   */
  const todayRatio = useMemo(() => {
    if (!bounds) return null;
    const offset = todayOffset(bounds, now);
    if (offset === null) return null;
    return offset / (bounds.end.getTime() - bounds.start.getTime());
  }, [bounds, now]);

  const scrollRef = useRef<HTMLDivElement>(null);
  /** The date fraction at the centre of the viewport, remembered so a zoom
   *  change magnifies around what the reader was looking at instead of
   *  throwing them back to the start of the project. */
  const centreRatio = useRef<number | null>(null);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el || el.scrollWidth <= 0) return;
    centreRatio.current = (el.scrollLeft + el.clientWidth / 2) / el.scrollWidth;
  }, []);

  const scrollToRatio = useCallback((ratio: number, smooth: boolean) => {
    const el = scrollRef.current;
    if (!el) return;
    const left = ratio * el.scrollWidth - el.clientWidth / 2;
    if (smooth && typeof el.scrollTo === "function") {
      el.scrollTo({ left, behavior: "smooth" });
    } else {
      el.scrollLeft = left;
    }
  }, []);

  // Runs on mount and after every width change. On mount there is no remembered
  // centre, so a long project opens at today rather than at its earliest date —
  // the far past is rarely what anyone came to look at.
  useLayoutEffect(() => {
    const ratio = centreRatio.current ?? todayRatio;
    if (ratio !== null) scrollToRatio(ratio, false);
  }, [width, todayRatio, scrollToRatio]);

  const scrollToToday = useCallback(() => {
    if (todayRatio !== null) scrollToRatio(todayRatio, true);
  }, [todayRatio, scrollToRatio]);

  return {
    zoom,
    setZoom,
    /** Width the bars area is drawn at; 0 when there is nothing to plot. */
    plotWidth: width,
    /** Attach to the horizontally scrolling wrapper around the chart. */
    scrollRef,
    onScroll,
    scrollToToday,
    /** False when today falls outside the plotted range — there is nowhere to go. */
    hasToday: todayRatio !== null,
  };
}
