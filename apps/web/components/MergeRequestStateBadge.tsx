import type { MergeRequestState } from "@/types";
import { Badge } from "@/components/ui/badge";

/**
 * MergeRequestStateBadge shows a merge request's GitLab state — read-only,
 * mirrored from GitLab (ADR-0011), unlike a task's status which FlowLens also
 * writes. Used in both the collection row and the single view so they never
 * disagree on what a given state reads as.
 */
export function MergeRequestStateBadge({ state }: { state: MergeRequestState }) {
  switch (state) {
    case "opened":
      return <Badge variant="default">Opened</Badge>;
    case "merged":
      return (
        <Badge variant="outline" className="border-primary/50 text-primary">
          Merged
        </Badge>
      );
    case "closed":
      return <Badge variant="secondary">Closed</Badge>;
    case "locked":
      return <Badge variant="outline">Locked</Badge>;
  }
}
