"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, GitlabConnection } from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

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
 * ConnectionForm creates or overwrites the project's GitLab connection. The
 * token field is always empty on open (even when overwriting) and is never
 * pre-filled — only the last four characters are ever shown, on
 * ConnectionStatus, once saved.
 */
function ConnectionForm({
  projectId,
  initialBaseUrl = "",
  onSaved,
  onCancel,
}: {
  projectId: string;
  initialBaseUrl?: string;
  onSaved: () => void;
  onCancel?: () => void;
}) {
  const router = useRouter();
  const [baseUrl, setBaseUrl] = useState(initialBaseUrl);
  const [token, setToken] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!baseUrl.trim() || !token.trim()) {
      setError("GitLab URL and access token are both required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/gitlab-connection`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ baseUrl, token }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to save the GitLab connection."));
        return;
      }
      setToken("");
      router.refresh();
      onSaved();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="Connect GitLab">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="gitlab-base-url" className="text-foreground block text-sm font-medium">
          GitLab URL
        </label>
        <Input
          id="gitlab-base-url"
          name="baseUrl"
          placeholder="https://gitlab.example.com"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label htmlFor="gitlab-token" className="text-foreground block text-sm font-medium">
          Access token
        </label>
        <Input
          id="gitlab-token"
          name="token"
          type="password"
          autoComplete="off"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Saving…" : "Save"}
        </Button>
        {onCancel ? (
          <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
        ) : null}
      </div>
    </form>
  );
}

function verifiedBadge(connection: GitlabConnection) {
  if (connection.lastVerifyError) {
    return (
      <Badge variant="outline" className="border-destructive text-destructive">
        Invalid
      </Badge>
    );
  }
  if (connection.verified) {
    return <Badge variant="secondary">Verified</Badge>;
  }
  return <Badge variant="outline">Not verified</Badge>;
}

/** ConnectionStatus shows the saved connection's details and its actions: test and reconnect. */
function ConnectionStatus({
  projectId,
  connection,
}: {
  projectId: string;
  connection: GitlabConnection;
}) {
  const router = useRouter();
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);
  const [reconnecting, setReconnecting] = useState(false);

  async function handleTest() {
    setTesting(true);
    setTestError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/gitlab-connection/test`, {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        setTestError(await parseError(res, "Connection test failed."));
      }
      router.refresh();
    } finally {
      setTesting(false);
    }
  }

  if (reconnecting) {
    return (
      <ConnectionForm
        projectId={projectId}
        initialBaseUrl={connection.baseUrl}
        onSaved={() => setReconnecting(false)}
        onCancel={() => setReconnecting(false)}
      />
    );
  }

  return (
    <div className="space-y-3">
      {connection.lastVerifyError ? (
        <Alert variant="destructive">
          <AlertDescription>{connection.lastVerifyError}</AlertDescription>
        </Alert>
      ) : null}
      {testError ? (
        <Alert variant="destructive">
          <AlertDescription>{testError}</AlertDescription>
        </Alert>
      ) : null}
      <dl className="grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">GitLab URL</dt>
          <dd className="text-foreground">{connection.baseUrl}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Status</dt>
          <dd>{verifiedBadge(connection)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Access token</dt>
          <dd className="text-foreground">•••• {connection.tokenLastFour}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Token owner</dt>
          <dd className="text-foreground">{connection.tokenGitlabUsername || "—"}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Last verified</dt>
          <dd className="text-foreground">
            {connection.lastVerifiedAt ? formatDateTime(connection.lastVerifiedAt) : "Never"}
          </dd>
        </div>
      </dl>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" onClick={handleTest} disabled={testing}>
          {testing ? "Testing…" : "Test connection"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => setReconnecting(true)}>
          Change connection
        </Button>
      </div>
    </div>
  );
}

/**
 * GitlabConnectionDetail is the single view of a project's GitLab connection.
 * There is at most one connection per project (ADR-0008), so this object has
 * no collection view — the project single view links straight here. Connect,
 * test and change all act on the connection itself (docs/ui-design.md rule 4);
 * the linked GitLab projects are a collection of their own, rendered below
 * this by the same screen.
 */
export function GitlabConnectionDetail({
  projectId,
  connection,
}: {
  projectId: string;
  connection: GitlabConnection | null;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">GitLab connection</CardTitle>
        {!connection ? (
          <CardDescription>Connect a GitLab CE instance to sync issues.</CardDescription>
        ) : null}
      </CardHeader>
      <CardContent>
        {connection ? (
          <ConnectionStatus projectId={projectId} connection={connection} />
        ) : (
          <ConnectionForm projectId={projectId} onSaved={() => {}} />
        )}
      </CardContent>
    </Card>
  );
}
