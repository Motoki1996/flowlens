"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { gitlabConnectionPath } from "@/lib/routes";
import type { ApiError, LinkedGitlabProject, SyncRun, SyncScope, WebhookEvent } from "@/types";
import { Card, CardHeader, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
        headers: csrfHeaders(),
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

/** parseLabels turns the comma-separated input into the API's syncLabels array. */
function parseLabels(input: string) {
  return input
    .split(",")
    .map((l) => l.trim())
    .filter(Boolean);
}

/**
 * SyncScopeSection shows the link's sync scope and swaps in a form to change
 * it. PATCH /linked-gitlab-projects/{linkId} rewrites the scope wholesale
 * rather than patching single fields, so every request here carries the scope
 * and the labels that go with it — and `isDefault`, which the API only ever
 * reads to *promote* a link (linkedproject.Service.Update), never to demote
 * one. Passing the link's current value therefore leaves the default alone.
 */
function SyncScopeSection({ link }: { link: LinkedGitlabProject }) {
  const router = useRouter();
  const [editing, setEditing] = useState(false);
  const [scope, setScope] = useState<SyncScope>(link.syncScope);
  const [labelsInput, setLabelsInput] = useState(link.syncLabels.join(", "));
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function cancel() {
    setScope(link.syncScope);
    setLabelsInput(link.syncLabels.join(", "));
    setError(null);
    setEditing(false);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const labels = parseLabels(labelsInput);
    if (scope === "labels" && labels.length === 0) {
      setError("Enter at least one label to sync by label.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${link.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          syncScope: scope,
          syncLabels: labels,
          isDefault: link.isDefault,
        }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to change the sync scope."));
        return;
      }
      router.refresh();
      setEditing(false);
    } finally {
      setPending(false);
    }
  }

  if (!editing) {
    return (
      <dd className="text-foreground flex flex-wrap items-center gap-2">
        {link.syncScope === "all" ? "All issues" : `Labels: ${link.syncLabels.join(", ")}`}
        <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
          Edit
        </Button>
      </dd>
    );
  }

  return (
    <dd>
      <form onSubmit={handleSubmit} className="space-y-2" aria-label="Edit sync scope">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <fieldset className="space-y-2 text-sm">
          <legend className="sr-only">Sync scope</legend>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="edit-sync-scope"
              checked={scope === "all"}
              onChange={() => setScope("all")}
            />
            All issues
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="edit-sync-scope"
              checked={scope === "labels"}
              onChange={() => setScope("labels")}
            />
            Only issues with specific labels
          </label>
          {scope === "labels" ? (
            <Input
              aria-label="Labels to sync"
              placeholder="bug, needs-triage"
              value={labelsInput}
              onChange={(e) => setLabelsInput(e.target.value)}
            />
          ) : null}
        </fieldset>
        <div className="flex gap-2">
          <Button type="submit" size="sm" disabled={pending}>
            {pending ? "Saving…" : "Save"}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={cancel} disabled={pending}>
            Cancel
          </Button>
        </div>
      </form>
    </dd>
  );
}

/**
 * DefaultSection reports whether this is its connection's default link and
 * offers to promote it when it isn't. The default is what a task's
 * assignee/label pickers read their candidates from (issue #80), so with more
 * than one link this is the only way to point them at the right project.
 */
function DefaultSection({ link }: { link: LinkedGitlabProject }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${link.id}`, {
        method: "PATCH",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          syncScope: link.syncScope,
          syncLabels: link.syncLabels,
          isDefault: true,
        }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to set this project as the default."));
        return;
      }
      router.refresh();
    } finally {
      setPending(false);
    }
  }

  if (link.isDefault) {
    return (
      <dd>
        <Badge variant="secondary">Default</Badge>
      </dd>
    );
  }

  return (
    <dd className="flex flex-wrap items-center gap-2">
      {error ? <span className="text-destructive text-xs">{error}</span> : null}
      <Button variant="outline" size="sm" onClick={handleClick} disabled={pending}>
        {pending ? "Setting…" : "Set as default"}
      </Button>
    </dd>
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
        headers: csrfHeaders(),
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
  webhookEventsNextPage = 0,
}: {
  projectId: string;
  link: LinkedGitlabProject;
  syncRuns?: SyncRun[];
  webhookEvents?: WebhookEvent[];
  // The API's paging cursor for the events above: 0 when there are no older
  // deliveries left to fetch.
  webhookEventsNextPage?: number;
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
              <SyncScopeSection link={link} />
            </div>
            <div>
              <dt className="text-muted-foreground">Default for this connection</dt>
              <DefaultSection link={link} />
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
        <WebhookEventSection linkId={link.id} events={webhookEvents} nextPage={webhookEventsNextPage} />
      </div>
    </>
  );
}
