"use client";

import type { Backlog } from "@/types";
import { GroupTimelineSection } from "@/components/GroupTimelineSection";

/**
 * BacklogTimelineSection is the Gantt-chart view mode of the Backlog
 * collection: the same backlogs BacklogListSection shows, laid out as bars
 * along a date axis (docs/ui-design.md rule 5).
 *
 * The chart itself is GroupTimelineSection, shared with the Epic collection —
 * see BacklogBoardSection for why the two objects share their view modes.
 */
export function BacklogTimelineSection({
  projectId,
  backlogs,
  now,
}: {
  projectId: string;
  backlogs: Backlog[];
  /** Injectable so stories and tests pin "today" instead of drifting with the clock. */
  now?: Date;
}) {
  return <GroupTimelineSection projectId={projectId} kind="backlog" items={backlogs} now={now} />;
}
