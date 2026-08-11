import { Badge } from "@/components/ui/badge";

/**
 * PipelineStatusBadge shows a merge request's latest known GitLab CI
 * pipeline status (mergerequest.MergeRequest.pipelineStatus, ADR-0011 §3).
 * An empty string means mrsync has not recorded a pipeline for this merge
 * request yet — renders nothing rather than a misleading "unknown" badge.
 */
export function PipelineStatusBadge({ status }: { status: string }) {
  if (!status) return null;
  switch (status) {
    case "success":
      return (
        <Badge variant="outline" className="border-primary/50 text-primary">
          Passed
        </Badge>
      );
    case "failed":
      return (
        <Badge variant="outline" className="border-destructive/50 text-destructive">
          Failed
        </Badge>
      );
    case "running":
    case "pending":
      return <Badge variant="secondary">Running</Badge>;
    case "canceled":
    case "skipped":
      return <Badge variant="outline">{status[0].toUpperCase() + status.slice(1)}</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}
