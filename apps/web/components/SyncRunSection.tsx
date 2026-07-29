"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, LinkedGitlabProject, SyncRun } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

function formatDateTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

async function parseError(res: Response, fallback: string) {
  const body = (await res.json().catch(() => null)) as ApiError | null;
  return body?.error.message ?? fallback;
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
 * SyncNowButton triggers a manual re-sync (POST
 * /linked-gitlab-projects/{linkId}/sync-runs) for one linked GitLab project.
 * A run already in progress reports 409, shown as an inline error rather
 * than a silent no-op.
 */
function SyncNowButton({ linkId }: { linkId: string }) {
  const router = useRouter();
  const [full, setFull] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${linkId}/sync-runs`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ full }),
      });
      if (!res.ok) {
        setError(res.status === 409 ? "A sync is already running." : await parseError(res, "Failed to start the sync."));
        return;
      }
      router.refresh();
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col items-end gap-1">
      {error ? <span className="text-destructive text-right text-xs">{error}</span> : null}
      <label className="text-muted-foreground flex items-center gap-1.5 text-xs">
        <input
          type="checkbox"
          checked={full}
          onChange={(e) => setFull(e.target.checked)}
          disabled={pending}
        />
        Full re-fetch
      </label>
      <Button variant="outline" size="sm" onClick={handleClick} disabled={pending}>
        {pending ? "Syncing…" : "Sync now"}
      </Button>
    </div>
  );
}

/**
 * SyncRunSection is the SyncRun collection embedded in the project single
 * view (docs/ui-design.md rule 4: "Sync now" is an action on the
 * LinkedGitlabProject object it acts on, not a standalone task screen).
 * One subsection per linked GitLab project, each with its own action and
 * history — a project can have more than one linked GitLab project.
 */
export function SyncRunSection({
  linkedProjects,
  syncRunsByLink,
}: {
  linkedProjects: LinkedGitlabProject[];
  syncRunsByLink: Record<string, SyncRun[]>;
}) {
  if (linkedProjects.length === 0) {
    return (
      <Card className="border-dashed">
        <CardHeader>
          <CardTitle className="text-base font-medium">Sync history</CardTitle>
          <CardDescription>Link a GitLab project to see its sync history here.</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Sync history</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        {linkedProjects.map((link) => (
          <div key={link.id} className="border-border space-y-3 border-t pt-4 first:border-t-0 first:pt-0">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-foreground text-sm font-medium">{link.pathWithNamespace}</h3>
              <SyncNowButton linkId={link.id} />
            </div>
            <SyncRunHistory runs={syncRunsByLink[link.id] ?? []} />
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
