"use client";

import type { Backlog } from "@/types";
import { GroupBoardSection } from "@/components/GroupBoardSection";

/**
 * BacklogBoardSection is the Board view mode of the Backlog collection: one
 * column per progress stage, cards stacked top to bottom inside it
 * (docs/ui-design.md rule 5 — the same dataset the List and Timeline modes
 * show).
 *
 * The board itself is GroupBoardSection, shared with the Epic collection: an
 * epic is deliberately a backlog that lives inside a backlog (ADR-0012), and
 * a board that only needs a name, a progress and a task ratio has nothing to
 * tell the two apart. This wrapper is what keeps the Backlog collection
 * naming its own component rather than reaching for a generic one.
 */
export function BacklogBoardSection({
  projectId,
  backlogs,
}: {
  projectId: string;
  backlogs: Backlog[];
}) {
  return <GroupBoardSection projectId={projectId} kind="backlog" items={backlogs} />;
}
