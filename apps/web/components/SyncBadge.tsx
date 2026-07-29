import type { TaskGitlabInfo } from "@/types";
import { Badge } from "@/components/ui/badge";

/**
 * SyncBadge shows a task's GitLab sync state, per docs/plans/issue-sync.md:
 * synced / pending (still syncing) / failed / null (never linked, local
 * only). Used in both the task collection row and the task single view so
 * the two never disagree on what a given task's badge should say.
 */
export function SyncBadge({ gitlab }: { gitlab: TaskGitlabInfo | null }) {
  if (!gitlab) {
    return <Badge variant="outline">Local only</Badge>;
  }
  switch (gitlab.syncStatus) {
    case "synced":
      return <Badge variant="default">Synced</Badge>;
    case "pending":
      return <Badge variant="secondary">Syncing…</Badge>;
    case "failed":
      return (
        <Badge variant="outline" className="border-destructive/50 text-destructive">
          Sync failed
        </Badge>
      );
  }
}
