"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, LinkedGitlabProject, WebhookEvent } from "@/types";
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

function statusBadge(status: WebhookEvent["status"]) {
  switch (status) {
    case "processed":
      return <Badge variant="secondary">Processed</Badge>;
    case "failed":
      return (
        <Badge variant="outline" className="border-destructive text-destructive">
          Failed
        </Badge>
      );
    case "skipped":
      return <Badge variant="outline">Skipped</Badge>;
    default:
      return <Badge variant="outline">Pending</Badge>;
  }
}

function skipReasonLabel(reason: string) {
  switch (reason) {
    case "stale":
      return "Stale delivery";
    case "echo":
      return "Echo of our own push";
    case "out_of_scope":
      return "Issue out of sync scope";
    case "unsupported_event":
      return "Unsupported event type";
    default:
      return reason;
  }
}

/** RetryEventButton retries one failed webhook event (POST .../webhook-events/{id}/retry). */
function RetryEventButton({ linkId, eventId }: { linkId: string; eventId: string }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${linkId}/webhook-events/${eventId}/retry`,
        { method: "POST", credentials: "include" },
      );
      if (!res.ok) {
        setError(await parseError(res, "Failed to retry the event."));
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

/** WebhookEventList shows one linked project's most recently received events, newest first. */
function WebhookEventList({ linkId, events }: { linkId: string; events: WebhookEvent[] }) {
  if (events.length === 0) {
    return <p className="text-muted-foreground text-sm">No webhook events received yet.</p>;
  }

  return (
    <ul className="space-y-2">
      {events.map((event) => (
        <li key={event.id} className="border-border rounded-md border px-3 py-2 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-foreground font-medium">
              {event.eventName || event.objectKind || "Webhook event"}
              {event.gitlabIssueIid ? ` #${event.gitlabIssueIid}` : ""}
            </span>
            {statusBadge(event.status)}
          </div>
          <p className="text-muted-foreground mt-1 text-xs">{formatDateTime(event.receivedAt)}</p>
          {event.status === "skipped" && event.skipReason ? (
            <p className="text-muted-foreground text-xs">Skipped: {skipReasonLabel(event.skipReason)}</p>
          ) : null}
          {event.status === "failed" ? (
            <div className="mt-1 flex flex-wrap items-center justify-between gap-2">
              {event.errorMessage ? <span className="text-destructive text-xs">{event.errorMessage}</span> : <span />}
              <RetryEventButton linkId={linkId} eventId={event.id} />
            </div>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

/**
 * WebhookEventSection is the WebhookEvent collection embedded in the project
 * single view (docs/ui-design.md rule 4), one subsection per linked GitLab
 * project — for troubleshooting inbound sync (issue #26). It warns whenever
 * any of the shown events failed to apply.
 */
export function WebhookEventSection({
  linkedProjects,
  webhookEventsByLink,
}: {
  linkedProjects: LinkedGitlabProject[];
  webhookEventsByLink: Record<string, WebhookEvent[]>;
}) {
  if (linkedProjects.length === 0) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Webhook events</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        {linkedProjects.map((link) => {
          const events = webhookEventsByLink[link.id] ?? [];
          const failedCount = events.filter((e) => e.status === "failed").length;
          return (
            <div key={link.id} className="border-border space-y-3 border-t pt-4 first:border-t-0 first:pt-0">
              <h3 className="text-foreground text-sm font-medium">{link.pathWithNamespace}</h3>
              {failedCount > 0 ? (
                <Alert variant="destructive">
                  <AlertDescription>
                    {failedCount} webhook event{failedCount > 1 ? "s" : ""} failed to apply. See the details below
                    and retry once the underlying issue is fixed.
                  </AlertDescription>
                </Alert>
              ) : null}
              <WebhookEventList linkId={link.id} events={events} />
            </div>
          );
        })}
      </CardContent>
    </Card>
  );
}
