"use client";

import type { Epic } from "@/types";
import { GroupBoardSection } from "@/components/GroupBoardSection";

/**
 * EpicBoardSection is the Board view mode of the Epic collection, the same
 * progress-axis board the Backlog collection shows (GroupBoardSection) — see
 * BacklogBoardSection for why the two share one implementation.
 */
export function EpicBoardSection({
  projectId,
  epics,
}: {
  projectId: string;
  epics: Epic[];
}) {
  return <GroupBoardSection projectId={projectId} kind="epic" items={epics} />;
}
