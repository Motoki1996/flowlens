"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, SyncJob } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
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

/** RetryJobButton retries one failed sync job (POST .../sync-jobs/{id}/retry). */
function RetryJobButton({ jobId }: { jobId: string }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/sync-jobs/${jobId}/retry`, {
        method: "POST",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to retry the job."));
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
      <Button variant="outline" size="sm" onClick={handleClick} disabled={pending}>
        {pending ? "Retrying…" : "Retry"}
      </Button>
    </div>
  );
}

/**
 * FailedSyncJobSection is the project single view's dead-letter list (issue
 * #97): a sync_jobs row that exhausted internal/sync's retry budget is
 * otherwise invisible outside the database, so this surfaces it and lets it
 * be retried directly by job ID — the project-scoped counterpart to
 * retrying an individual task's sync from the Task view.
 */
export function FailedSyncJobSection({ jobs }: { jobs: SyncJob[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">
          Failed sync jobs
          {jobs.length > 0 ? (
            <span className="text-muted-foreground ml-1 font-normal">({jobs.length})</span>
          ) : null}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {jobs.length > 0 ? (
          <Alert variant="destructive">
            <AlertDescription>
              {jobs.length} sync job{jobs.length > 1 ? "s" : ""} permanently failed to push to GitLab. See the
              details below and retry once the underlying issue is fixed.
            </AlertDescription>
          </Alert>
        ) : null}
        {jobs.length === 0 ? (
          <p className="text-muted-foreground text-sm">No failed sync jobs.</p>
        ) : (
          <ul className="space-y-2">
            {jobs.map((job) => (
              <li key={job.id} className="border-border rounded-md border px-3 py-2 text-sm">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-foreground font-medium">{job.kind}</span>
                  <Badge variant="outline" className="border-destructive text-destructive">
                    Failed
                  </Badge>
                </div>
                <p className="text-muted-foreground mt-1 text-xs">
                  {job.attempts} attempt{job.attempts !== 1 ? "s" : ""} · last tried{" "}
                  {formatDateTime(job.updatedAt)}
                </p>
                <div className="mt-1 flex flex-wrap items-center justify-between gap-2">
                  {job.lastError ? (
                    <span className="text-destructive text-xs">{job.lastError}</span>
                  ) : (
                    <span />
                  )}
                  <RetryJobButton jobId={job.id} />
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
