"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import { linkedGitlabProjectPath } from "@/lib/routes";
import type { ApiError, GitlabProjectOption, LinkedGitlabProject, SyncScope } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { CreateFormRegion } from "@/components/CreateFormRegion";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { WebhookBadge } from "@/components/WebhookBadge";
import { DefaultLinkBadge } from "@/components/DefaultLinkBadge";

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
  // The API's own paging cursor for the search above: 0 once there is nothing
  // left to fetch. A token with access to many GitLab projects would
  // otherwise only ever be offered the first page.
  const [nextPage, setNextPage] = useState(0);
  const [selected, setSelected] = useState<GitlabProjectOption | null>(null);
  const [scope, setScope] = useState<SyncScope>("all");
  const [labelsInput, setLabelsInput] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  // page 0 means "the first page" and replaces the current results; anything
  // else appends, so "Show more" extends the list the user is looking at.
  async function runSearch(term: string, page = 0) {
    setLoading(true);
    setSearchError(null);
    try {
      const params = new URLSearchParams();
      if (term.trim()) params.set("search", term.trim());
      if (page > 0) params.set("page", String(page));
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/projects/${projectId}/gitlab-connection/available-projects?${params}`,
        { credentials: "include" },
      );
      if (!res.ok) {
        setSearchError(await parseError(res, "Failed to list GitLab projects."));
        if (page === 0) {
          setOptions([]);
          setNextPage(0);
        }
        return;
      }
      const body = (await res.json()) as { projects: GitlabProjectOption[]; nextPage: number };
      setOptions((current) => (page === 0 ? body.projects : [...current, ...body.projects]));
      setNextPage(body.nextPage);
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
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
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
      {nextPage > 0 ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => void runSearch(search, nextPage)}
          disabled={loading}
        >
          {loading ? "Loading…" : "Show more"}
        </Button>
      ) : null}
    </div>
  );
}

/**
 * LinkedGitlabProjectListSection is the LinkedGitlabProject collection,
 * rendered on the GitLab connection screen because a link only exists through
 * a connection. Rows carry the status a reader scans for; everything that acts
 * on one link (sync, webhook registration, unlink) lives on its single view.
 */
export function LinkedGitlabProjectListSection({
  projectId,
  links,
  connected,
}: {
  projectId: string;
  links: LinkedGitlabProject[];
  /** Without a connection there is nothing to search, so linking is hidden. */
  connected: boolean;
}) {
  const [linking, setLinking] = useState(false);

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Linked GitLab projects</CardTitle>
          {connected && !linking ? (
            <Button variant="outline" size="sm" onClick={() => setLinking(true)}>
              Link a project
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {linking ? (
          <CreateFormRegion>
            <LinkProjectForm
              projectId={projectId}
              onDone={() => setLinking(false)}
              onCancel={() => setLinking(false)}
            />
          </CreateFormRegion>
        ) : null}
        {!connected ? (
          <p className="text-muted-foreground text-sm">
            Connect GitLab above to link a project and start syncing issues.
          </p>
        ) : links.length === 0 ? (
          <p className="text-muted-foreground text-sm">No linked GitLab projects yet.</p>
        ) : (
          <ul className="space-y-2">
            {links.map((link) => (
              <li key={link.id}>
                <Link
                  href={linkedGitlabProjectPath(projectId, link.id)}
                  className="border-border hover:border-ring flex flex-wrap items-start justify-between gap-3 rounded-md border px-3 py-2 transition-colors"
                >
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-foreground text-sm font-medium">
                        {link.pathWithNamespace}
                      </span>
                      <DefaultLinkBadge isDefault={link.isDefault} />
                    </div>
                    <p className="text-muted-foreground text-xs">
                      {link.syncScope === "all"
                        ? "All issues"
                        : `Labels: ${link.syncLabels.join(", ")}`}
                    </p>
                    <p className="text-muted-foreground text-xs">
                      Last synced: {link.lastSyncedAt ? formatDateTime(link.lastSyncedAt) : "Never"}
                    </p>
                  </div>
                  <WebhookBadge status={link.webhookStatus} />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
