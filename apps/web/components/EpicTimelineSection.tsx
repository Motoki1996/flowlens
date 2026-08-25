"use client";

import type { Epic } from "@/types";
import { GroupTimelineSection } from "@/components/GroupTimelineSection";

/**
 * EpicTimelineSection is the Gantt-chart view mode of the Epic collection,
 * the same chart the Backlog collection shows (GroupTimelineSection).
 */
export function EpicTimelineSection({
  projectId,
  epics,
  now,
}: {
  projectId: string;
  epics: Epic[];
  /** Injectable so stories and tests pin "today" instead of drifting with the clock. */
  now?: Date;
}) {
  return <GroupTimelineSection projectId={projectId} kind="epic" items={epics} now={now} />;
}
