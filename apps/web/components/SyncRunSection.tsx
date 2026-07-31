import type { SyncRun } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function statusBadge(status: SyncRun["status"]) {
  switch (status) {
    case "succeeded":
      return <Badge variant="secondary">Succeeded</Badge>;
    case "failed":
      return (
        <Badge variant="outline" className="border-destructive text-destructive">
          Failed
        </Badge>
      );
    default:
      return <Badge variant="outline">Running…</Badge>;
  }
}

function kindLabel(kind: SyncRun["kind"]) {
  return kind === "initial_import" ? "Initial import" : "Manual resync";
}

/** SyncRunHistory lists one linked project's sync runs, newest first. */
function SyncRunHistory({ runs }: { runs: SyncRun[] }) {
  if (runs.length === 0) {
    return <p className="text-muted-foreground text-sm">No sync runs yet.</p>;
  }

  return (
    <ul className="space-y-2">
      {runs.map((run) => (
        <li key={run.id} className="border-border rounded-md border px-3 py-2 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-foreground font-medium">{kindLabel(run.kind)}</span>
            {statusBadge(run.status)}
          </div>
          <p className="text-muted-foreground mt-1 text-xs">
            {run.issuesSeen} seen, {run.issuesCreated} created, {run.issuesUpdated} updated
          </p>
          <p className="text-muted-foreground text-xs">
            {run.startedAt ? formatDateTime(run.startedAt) : "Not started"}
            {run.completedAt ? ` – ${formatDateTime(run.completedAt)}` : ""}
          </p>
          {run.status === "failed" && run.errorMessage ? (
            <p className="text-destructive mt-1 text-xs">{run.errorMessage}</p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

/**
 * SyncRunSection is the SyncRun collection of one linked GitLab project,
 * rendered inside that link's single view. "Sync now" is an action on the
 * LinkedGitlabProject rather than on its history, so it lives in the link's
 * own header (docs/ui-design.md rule 4).
 */
export function SyncRunSection({ runs }: { runs: SyncRun[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Sync history</CardTitle>
      </CardHeader>
      <CardContent>
        <SyncRunHistory runs={runs} />
      </CardContent>
    </Card>
  );
}
