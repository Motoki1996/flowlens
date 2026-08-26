"use client";

import { useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { TruncatedName } from "@/components/TruncatedName";
import type { DateRange, GanttRow } from "@/lib/timeline";
import type { useTimelineViewport } from "@/lib/useTimelineViewport";
import {
  AXIS_HEIGHT,
  GanttChart,
  NAME_COLUMN_CLASS,
  ROW_HEIGHT,
  rowBandStyle,
} from "@/components/GanttChart";

/** The name column's bounds, in px. The minimum is narrow enough to be worth
 *  dragging to when the plot is what matters and the names are only a key. */
const MIN_NAME_WIDTH = 120;
const MAX_NAME_WIDTH = 560;
/** …and however wide the column is dragged, this much plot stays on screen:
 *  on a phone the maximum above is most of the viewport, and a reader who has
 *  dragged the chart off the edge has to guess how to get it back. */
const MIN_PLOT_VISIBLE = 240;
/** What a keyboard nudge moves, and the width the handle assumes before the
 *  column has been measured (matching NAME_COLUMN_CLASS's `sm:w-64`). */
const NAME_WIDTH_STEP = 16;
const DEFAULT_NAME_WIDTH = 256;

/**
 * TimelineFrame is the layout every Timeline view mode shares: a column of
 * names, and beside it the plot those names index, scrolling horizontally on
 * its own. The Task and the Backlog/Epic timelines differ only in what sits
 * under each name, which arrives as `meta` — everything else about the two has
 * to stay identical, and did so previously by being written out twice.
 *
 * Names are a real column of <Link>s rather than SVG axis labels so each one
 * navigates and is reachable by keyboard; the trade is that the column has a
 * fixed width and long names truncate, which is why the reader can drag it
 * wider. Like zoom, that width is local view state — see useTimelineViewport.
 */
export function TimelineFrame({
  rows,
  bounds,
  now,
  viewport,
  href,
  meta,
}: {
  rows: GanttRow[];
  bounds: DateRange;
  now: Date;
  viewport: ReturnType<typeof useTimelineViewport>;
  /** Where a name and its bar both go — the single view of whatever the row is. */
  href: (row: GanttRow) => string;
  /** The line under a name: a priority flag, a completion ratio, predecessors.
   *  An empty flex row is zero-height, so a row with nothing to add keeps its
   *  title vertically centred. */
  meta: (row: GanttRow) => ReactNode;
}) {
  // null means "whatever the viewport calls for" (NAME_COLUMN_CLASS) — a
  // dragged width is a px value that then holds at every breakpoint, because
  // the reader has said what they want and a media query undoing it would read
  // as the column snapping back on its own.
  const [nameWidth, setNameWidth] = useState<number | null>(null);
  const nameRef = useRef<HTMLDivElement>(null);

  const clampWidth = (width: number) => {
    const frame = nameRef.current?.parentElement?.getBoundingClientRect().width ?? 0;
    const max = frame > 0 ? Math.min(MAX_NAME_WIDTH, frame - MIN_PLOT_VISIBLE) : MAX_NAME_WIDTH;
    return Math.min(Math.max(MIN_NAME_WIDTH, max), Math.max(MIN_NAME_WIDTH, width));
  };

  /** The width a drag or a nudge starts from: whatever is on screen now. */
  const currentWidth = () =>
    nameWidth ?? (nameRef.current?.getBoundingClientRect().width || DEFAULT_NAME_WIDTH);

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    // The pointer leaves the 4px handle immediately, so the drag is tracked on
    // the window rather than on the element it started from.
    event.preventDefault();
    const originX = event.clientX;
    const originWidth = currentWidth();
    const move = (e: globalThis.PointerEvent) =>
      setNameWidth(clampWidth(originWidth + e.clientX - originX));
    const stop = () => {
      document.body.style.userSelect = "";
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", stop);
    };
    // Without this, dragging left across the names selects them, and the drag
    // ends with half the column highlighted.
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", stop);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const step =
      event.key === "ArrowLeft" ? -NAME_WIDTH_STEP : event.key === "ArrowRight" ? NAME_WIDTH_STEP : 0;
    if (step === 0) return;
    event.preventDefault();
    setNameWidth(clampWidth(currentWidth() + step));
  };

  return (
    <div className="flex">
      <div
        ref={nameRef}
        className={nameWidth === null ? `shrink-0 ${NAME_COLUMN_CLASS}` : "shrink-0"}
        style={{
          ...rowBandStyle(AXIS_HEIGHT),
          ...(nameWidth === null ? {} : { width: nameWidth }),
        }}
      >
        {/* Spacer keeping the first name aligned with the first bar, not with the date axis. */}
        <div style={{ height: AXIS_HEIGHT }} />
        <ul>
          {rows.map((row) => (
            <li
              key={row.id}
              className="flex flex-col justify-center pr-3"
              style={{ height: ROW_HEIGHT }}
            >
              {/* The title gets the line to itself: sharing it with a priority
                  and a progress pill left it a few dozen pixels and every row
                  read as an ellipsis. Both fields are on the bar's tooltip
                  instead, with high/urgent still flagged below so a scan
                  doesn't have to hover to find it. */}
              <TruncatedName
                href={href(row)}
                text={row.title}
                className="text-foreground text-sm hover:underline"
              />
              <div className="flex min-w-0 items-center gap-1.5 text-xs">{meta(row)}</div>
            </li>
          ))}
        </ul>
      </div>

      {/* A focusable separator is the splitter pattern: dragging is the obvious
          gesture, arrow keys are the one that works without a pointer, and a
          double-click hands the width back to the viewport. */}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize name column"
        aria-valuenow={Math.round(nameWidth ?? DEFAULT_NAME_WIDTH)}
        aria-valuemin={MIN_NAME_WIDTH}
        aria-valuemax={MAX_NAME_WIDTH}
        tabIndex={0}
        title="Drag to resize the name column; double-click to reset"
        // The ::after pseudo-element widens the grab area either side without
        // widening the seam itself, which stays 4px so it reads as a divider
        // rather than as a third column.
        className="hover:bg-border focus-visible:bg-ring relative w-1 shrink-0 cursor-col-resize touch-none rounded-full after:absolute after:inset-y-0 after:-left-1 after:-right-1 after:content-[''] focus-visible:outline-none"
        onPointerDown={onPointerDown}
        onKeyDown={onKeyDown}
        onDoubleClick={() => setNameWidth(null)}
      />

      <div
        ref={viewport.scrollRef}
        onScroll={viewport.onScroll}
        className="min-w-0 flex-1 overflow-x-auto"
      >
        <div style={{ minWidth: viewport.plotWidth, ...rowBandStyle(AXIS_HEIGHT) }}>
          <GanttChart rows={rows} bounds={bounds} now={now} zoom={viewport.zoom} href={href} />
        </div>
      </div>
    </div>
  );
}
