"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type {
  ApiError,
  GitlabConnection,
  GitlabProjectOption,
  LinkedGitlabProject,
  SyncScope,
} from "@/types";
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

function WebhookBadge({ link }: { link: LinkedGitlabProject }) {
  switch (link.webhookStatus) {
    case "registered":
      return <Badge variant="secondary">Webhook registered</Badge>;
    case "failed":
      return (
        <Badge variant="outline" className="border-destructive text-destructive">
          Webhook failed
        </Badge>
      );
    default:
      return <Badge variant="outline">Webhook not registered</Badge>;
  }
}

/** RegisterWebhookButton (re)registers a link's webhook; safe to retry, never creates a duplicate. */
function RegisterWebhookButton({ link }: { link: LinkedGitlabProject }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleClick() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${link.id}/webhook`, {
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

/** UnlinkButton interposes an inline confirmation before unlinking a GitLab project. */
function UnlinkButton({ link }: { link: LinkedGitlabProject }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleUnlink() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/linked-gitlab-projects/${link.id}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok && res.status !== 204) {
        setError(await parseError(res, "Failed to unlink the project."));
        setPending(false);
        return;
      }
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

function LinkedProjectsList({ links }: { links: LinkedGitlabProject[] }) {
  if (links.length === 0) {
    return <p className="text-muted-foreground text-sm">No linked GitLab projects yet.</p>;
  }

  return (
    <ul className="space-y-2">
      {links.map((link) => (
        <li key={link.id} className="border-border rounded-md border px-3 py-2">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <a
                href={link.webUrl}
                target="_blank"
                rel="noreferrer"
                className="text-foreground text-sm font-medium hover:underline"
              >
                {link.pathWithNamespace}
              </a>
              <p className="text-muted-foreground text-xs">
                {link.syncScope === "all" ? "All issues" : `Labels: ${link.syncLabels.join(", ")}`}
              </p>
              <p className="text-muted-foreground text-xs">
                Last synced: {link.lastSyncedAt ? formatDateTime(link.lastSyncedAt) : "Never"}
              </p>
            </div>
            <div className="flex flex-col items-end gap-2">
              <WebhookBadge link={link} />
              {link.webhookStatus !== "registered" ? (
                <>
                  {link.webhookError ? (
                    <span className="text-destructive max-w-64 text-right text-xs">{link.webhookError}</span>
                  ) : null}
                  <RegisterWebhookButton link={link} />
                </>
              ) : null}
              <UnlinkButton link={link} />
            </div>
          </div>
        </li>
      ))}
    </ul>
  );
}

/**
 * LinkProjectForm searches the connection's available GitLab projects, then
 * (once one is picked) collects its sync scope before linking it.
 */
function LinkProjectForm({
  projectId,
  onDone,
  onCancel,
}: {
  projectId: string;
  onDone: () => void;
  onCancel: () => void;
}) {
  const router = useRouter();
  const [search, setSearch] = useState("");
  const [options, setOptions] = useState<GitlabProjectOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [selected, setSelected] = useState<GitlabProjectOption | null>(null);
  const [scope, setScope] = useState<SyncScope>("all");
  const [labelsInput, setLabelsInput] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function runSearch(term: string) {
    setLoading(true);
    setSearchError(null);
    try {
      const params = new URLSearchParams();
      if (term.trim()) params.set("search", term.trim());
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/projects/${projectId}/gitlab-connection/available-projects?${params}`,
        { credentials: "include" },
      );
      if (!res.ok) {
        setSearchError(await parseError(res, "Failed to list GitLab projects."));
        setOptions([]);
        return;
      }
      const body = (await res.json()) as { projects: GitlabProjectOption[] };
      setOptions(body.projects);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void runSearch("");
    // Only search on mount; further searches are user-triggered via the form.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleSearchSubmit(e: FormEvent) {
    e.preventDefault();
    await runSearch(search);
  }

  async function handleLink(e: FormEvent) {
    e.preventDefault();
    if (!selected) return;
    const labels = labelsInput
      .split(",")
      .map((l) => l.trim())
      .filter(Boolean);
    if (scope === "labels" && labels.length === 0) {
      setError("Enter at least one label to sync by label.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/linked-gitlab-projects`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ gitlabProjectId: selected.id, syncScope: scope, syncLabels: labels }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to link the project."));
        return;
      }
      router.refresh();
      onDone();
    } finally {
      setPending(false);
    }
  }

  if (selected) {
    return (
      <form onSubmit={handleLink} className="space-y-3" aria-label="Set sync scope">
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <p className="text-foreground text-sm">
          Linking <span className="font-medium">{selected.pathWithNamespace}</span>
        </p>
        <fieldset className="space-y-2 text-sm">
          <legend className="text-foreground font-medium">Sync scope</legend>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="sync-scope"
              checked={scope === "all"}
              onChange={() => setScope("all")}
            />
            All issues
          </label>
          <label className="flex items-center gap-2">
            <input
              type="radio"
              name="sync-scope"
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
            {pending ? "Linking…" : "Link project"}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => setSelected(null)} disabled={pending}>
            Back
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
        </div>
      </form>
    );
  }

  return (
    <div className="space-y-3">
      <form onSubmit={handleSearchSubmit} className="flex flex-wrap gap-2">
        <Input
          aria-label="Search GitLab projects"
          placeholder="Search projects…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Button type="submit" size="sm" disabled={loading}>
          {loading ? "Searching…" : "Search"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </form>
      {searchError ? (
        <Alert variant="destructive">
          <AlertDescription>{searchError}</AlertDescription>
        </Alert>
      ) : null}
      {!searchError && !loading && options.length === 0 ? (
        <p className="text-muted-foreground text-sm">No GitLab projects found.</p>
      ) : null}
      {options.length > 0 ? (
        <ul className="space-y-1">
          {options.map((opt) => (
            <li key={opt.id}>
              <button
                type="button"
                onClick={() => setSelected(opt)}
                className="border-border hover:bg-accent w-full rounded-md border px-3 py-2 text-left text-sm"
              >
                {opt.pathWithNamespace}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/**
 * GitlabConnectionSection is the GitLab connection object embedded in the
 * project single view (docs/ui-design.md rules 4 and 7 — no standalone
 * "setup" screen). It covers connecting, testing, and managing linked GitLab
 * projects and their webhook registration.
 */
export function GitlabConnectionSection({
  projectId,
  connection,
  linkedProjects,
}: {
  projectId: string;
  connection: GitlabConnection | null;
  linkedProjects: LinkedGitlabProject[];
}) {
  const [linking, setLinking] = useState(false);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">GitLab connection</CardTitle>
        {!connection ? (
          <CardDescription>Connect a GitLab CE instance to sync issues.</CardDescription>
        ) : null}
      </CardHeader>
      <CardContent className="space-y-6">
        {connection ? (
          <ConnectionStatus projectId={projectId} connection={connection} />
        ) : (
          <ConnectionForm projectId={projectId} onSaved={() => {}} />
        )}

        {connection ? (
          <div className="border-border space-y-3 border-t pt-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-foreground text-sm font-medium">Linked projects</h3>
              {!linking ? (
                <Button variant="outline" size="sm" onClick={() => setLinking(true)}>
                  Link a project
                </Button>
              ) : null}
            </div>
            {linking ? (
              <LinkProjectForm
                projectId={projectId}
                onDone={() => setLinking(false)}
                onCancel={() => setLinking(false)}
              />
            ) : null}
            <LinkedProjectsList links={linkedProjects} />
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
