"use client";

import { GanttChartSquare, LayoutGrid, List } from "lucide-react";
import { Button } from "@/components/ui/button";

/** The three view modes the Task and the Backlog collection share
 *  (docs/ui-design.md rule 5 — one screen, several presentations). */
export type ViewMode = "board" | "list" | "timeline";

// Icon-only, because the group is a presentation switch rather than an action
// on the object: it sits next to the collection's filters and its "New …"
// button, and spelling all three out crowded them out of the header row. The
// label survives as the accessible name and as the hover tooltip, so the
// buttons stay findable by name for screen readers and tests alike.
const MODES: { mode: ViewMode; label: string; Icon: typeof LayoutGrid }[] = [
  { mode: "board", label: "Board", Icon: LayoutGrid },
  { mode: "list", label: "List", Icon: List },
  { mode: "timeline", label: "Timeline", Icon: GanttChartSquare },
];

export function ViewModeToggle({
  value,
  onChange,
}: {
  value: ViewMode;
  onChange: (mode: ViewMode) => void;
}) {
  return (
    <div className="flex" role="group" aria-label="View">
      {MODES.map(({ mode, label, Icon }, index) => (
        <Button
          key={mode}
          type="button"
          variant={value === mode ? "default" : "outline"}
          size="sm"
          aria-label={label}
          aria-pressed={value === mode}
          title={label}
          className={
            index === 0
              ? "rounded-r-none px-2"
              : index === MODES.length - 1
                ? "rounded-l-none px-2"
                : "rounded-none px-2"
          }
          onClick={() => onChange(mode)}
        >
          <Icon className="size-4" />
        </Button>
      ))}
    </div>
  );
}
