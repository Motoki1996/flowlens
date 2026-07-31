"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { gitlabConnectionPath } from "@/lib/routes";
import type { ApiError, LinkedGitlabProject, SyncRun, WebhookEvent } from "@/types";
import { Card, CardHeader, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { WebhookBadge } from "@/components/WebhookBadge";
import { SyncRunSection } from "@/components/SyncRunSection";
import { WebhookEventSection } from "@/components/WebhookEventSection";

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

/**
 * SyncNowButton triggers a manual re-sync (POST
 * /linked-gitlab-projects/{linkId}/sync-runs). A run already in progress
 * reports 409, shown as an inline error rather than a silent no-op.
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

/** RegisterWebhookButton (re)registers the link's webhook; safe to retry, never creates a duplicate. */
function RegisterWebhookButton({ linkId }: { linkId: string }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${linkId}/webhook`, {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to register the webhook."));
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
        {pending ? "Registering…" : "Register webhook"}
      </Button>
    </div>
  );
}

/**
 * UnlinkButton interposes an inline confirmation before unlinking. Unlinking
 * removes the object this screen is about, so it returns to the connection
 * the link belonged to rather than leaving a dead page behind.
 */
function UnlinkButton({ projectId, linkId }: { projectId: string; linkId: string }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleUnlink() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${linkId}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok && res.status !== 204) {
        setError(await parseError(res, "Failed to unlink the project."));
        setPending(false);
        return;
      }
      router.push(gitlabConnectionPath(projectId));
      router.refresh();
    } catch {
      setPending(false);
    }
  }

  if (confirming) {
    return (
      <div className="flex flex-col items-end gap-1">
        {error ? <span className="text-destructive text-xs">{error}</span> : null}
        <span className="text-foreground text-xs">Unlink this project?</span>
        <div className="flex gap-2">
          <Button variant="destructive" size="sm" onClick={handleUnlink} disabled={pending}>
            {pending ? "Unlinking…" : "Confirm unlink"}
          </Button>
          <Button variant="outline" size="sm" onClick={() => setConfirming(false)} disabled={pending}>
            Cancel
          </Button>
        </div>
      </div>
    );
  }

  return (
    <Button variant="outline" size="sm" onClick={() => setConfirming(true)}>
      Unlink
    </Button>
  );
}

/**
 * LinkedGitlabProjectDetail is the single view for one linked GitLab project:
 * identity, attributes, its actions, then its related collections (sync runs
 * and webhook events), per docs/ui-design.md rule 6. SyncRun and WebhookEvent
 * appear only here — they are never browsed apart from the link they belong
 * to, so neither gets routes of its own.
 */
export function LinkedGitlabProjectDetail({
  projectId,
  link,
  syncRuns = [],
  webhookEvents = [],
}: {
  projectId: string;
  link: LinkedGitlabProject;
  syncRuns?: SyncRun[];
  webhookEvents?: WebhookEvent[];
}) {
  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h1 className="text-foreground text-xl leading-none font-semibold">
                {link.pathWithNamespace}
              </h1>
              <CardDescription className="mt-1.5">
                <a href={link.webUrl} target="_blank" rel="noreferrer" className="hover:underline">
                  {link.webUrl}
                </a>
              </CardDescription>
            </div>
            <div className="flex shrink-0 flex-col items-end gap-2">
              <SyncNowButton linkId={link.id} />
              <UnlinkButton projectId={projectId} linkId={link.id} />
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {link.webhookStatus === "failed" && link.webhookError ? (
            <Alert variant="destructive">
              <AlertDescription>{link.webhookError}</AlertDescription>
            </Alert>
          ) : null}
          <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">Sync scope</dt>
              <dd className="text-foreground">
                {link.syncScope === "all" ? "All issues" : `Labels: ${link.syncLabels.join(", ")}`}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Last synced</dt>
              <dd className="text-foreground">
                {link.lastSyncedAt ? formatDateTime(link.lastSyncedAt) : "Never"}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Webhook</dt>
              <dd className="flex flex-wrap items-center gap-2">
                <WebhookBadge status={link.webhookStatus} />
                {link.webhookStatus !== "registered" ? (
                  <RegisterWebhookButton linkId={link.id} />
                ) : null}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <div className="mt-8">
        <SyncRunSection runs={syncRuns} />
      </div>

      <div className="mt-8">
        <WebhookEventSection linkId={link.id} events={webhookEvents} />
      </div>
    </>
  );
}
