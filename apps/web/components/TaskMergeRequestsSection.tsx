import Link from "next/link";
import { mergeRequestPath } from "@/lib/routes";
import type { MergeRequest } from "@/types";
import { MergeRequestStateBadge } from "@/components/MergeRequestStateBadge";
import { PipelineStatusBadge } from "@/components/PipelineStatusBadge";
import { TruncatedName } from "@/components/TruncatedName";

/**
 * TaskMergeRequestsSection is the Task single view's reverse link to the
 * MergeRequest object (issue #112): the merge request(s) whose description
 * or branch name referenced this task's linked GitLab issue (mrsync). A task
 * has no action over a merge request — FlowLens never writes one back to
 * GitLab (ADR-0011) — so this is a read-only list, each row linking out to
 * that merge request's own single view.
 */
export function TaskMergeRequestsSection({
  projectId,
  mergeRequests,
}: {
  projectId: string;
  mergeRequests: MergeRequest[];
}) {
  if (mergeRequests.length === 0) {
    return <p className="text-muted-foreground text-sm">No merge requests reference this task.</p>;
  }

  return (
    <ul className="space-y-2">
      {mergeRequests.map((mr) => (
        <li key={mr.id}>
          <Link
            href={mergeRequestPath(projectId, mr.id)}
            className="border-border hover:border-ring flex items-center justify-between gap-4 rounded-md border px-3 py-2 text-sm transition-colors"
          >
            <TruncatedName
              text={`!${mr.number} ${mr.title}`}
              className="text-foreground"
            />
            <span className="flex shrink-0 items-center gap-2 text-xs">
              <PipelineStatusBadge status={mr.pipelineStatus} />
              <MergeRequestStateBadge state={mr.state} />
            </span>
          </Link>
        </li>
      ))}
    </ul>
  );
}
