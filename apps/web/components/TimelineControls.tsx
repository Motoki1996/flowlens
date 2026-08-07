"use client";

import { Button } from "@/components/ui/button";
import { TIMELINE_ZOOMS, ZOOM_LEVELS, type TimelineZoom } from "@/lib/timeline";

/**
 * TimelineControls is the pair of controls a Gantt chart needs once its range
 * outgrows the screen: a zoom, and a way back to today.
 *
 * They act on the *presentation*, not on the object, so they sit inside the
 * timeline itself rather than in the collection header beside "New task" —
 * docs/ui-design.md rule 3 keeps object actions and view controls apart.
 *
 * The zoom levels are named after the tick interval they afford (Month, Week,
 * Day) rather than "in"/"out": a magnifier says nothing about what you will be
 * able to read, and the label is what tells you.
 */
export function TimelineControls({
  zoom,
  onZoomChange,
  onToday,
  hasToday,
}: {
  zoom: TimelineZoom;
  onZoomChange: (zoom: TimelineZoom) => void;
  onToday: () => void;
  /** False when today is outside the plotted range, which disables the button
   *  rather than hiding it — a control that vanishes reads as a bug. */
  hasToday: boolean;
}) {
  return (
    <div className="flex items-center gap-2">
      <div className="flex" role="group" aria-label="Zoom">
        {TIMELINE_ZOOMS.map((level, index) => (
          <Button
            key={level}
            type="button"
            variant={zoom === level ? "default" : "outline"}
            size="sm"
            aria-pressed={zoom === level}
            className={
              index === 0
                ? "rounded-r-none px-2"
                : index === TIMELINE_ZOOMS.length - 1
                  ? "rounded-l-none px-2"
                  : "rounded-none px-2"
            }
            onClick={() => onZoomChange(level)}
          >
            {ZOOM_LEVELS[level].label}
          </Button>
        ))}
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="px-2"
        disabled={!hasToday}
        title={hasToday ? undefined : "Today is outside the scheduled range"}
        onClick={onToday}
      >
        Today
      </Button>
    </div>
  );
}
