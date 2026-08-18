"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { HelpCircleIcon, SquareArrowOutUpRightIcon } from "lucide-react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, GitlabIdentity } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

/** Strips a trailing slash so `${base}/api/v4/user` never doubles up. */
function normalizeBaseUrl(baseUrl: string) {
  return baseUrl.trim().replace(/\/+$/, "");
}

async function parseError(res: Response, fallback: string) {
  const body = (await res.json().catch(() => null)) as ApiError | null;
  return body?.error.message ?? fallback;
}

/**
 * RegisterIdentityForm registers or replaces the caller's GitLab identity
 * for one GitLab base URL (issue #102). No access token is collected here —
 * only the identifiers ?assignee=me needs to match a task's own assignee
 * fields; the access token used for sync is a distinct, project-scoped
 * feature (ADR-0008).
 */
function RegisterIdentityForm({ onSaved }: { onSaved: () => void }) {
  const [baseUrl, setBaseUrl] = useState("");
  const [gitlabUserId, setGitlabUserId] = useState("");
  const [gitlabUsername, setGitlabUsername] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!baseUrl.trim() || !gitlabUserId.trim()) {
      setError("GitLab base URL and user ID are required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/me/gitlab-identities`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({
          gitlabBaseUrl: baseUrl.trim(),
          gitlabUserId: Number(gitlabUserId),
          gitlabUsername: gitlabUsername.trim(),
        }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to save the GitLab identity."));
        return;
      }
      setBaseUrl("");
      setGitlabUserId("");
      setGitlabUsername("");
      onSaved();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="Register GitLab identity">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="gitlab-identity-base-url" className="text-foreground block text-sm font-medium">
          GitLab base URL
        </label>
        <Input
          id="gitlab-identity-base-url"
          placeholder="https://gitlab.example.com"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <div className="flex items-center gap-1">
          <label htmlFor="gitlab-identity-user-id" className="text-foreground block text-sm font-medium">
            GitLab user ID
          </label>
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground"
                aria-label="How to find your GitLab user ID"
              >
                <HelpCircleIcon className="size-3.5" />
              </button>
            </PopoverTrigger>
            <PopoverContent className="w-80 text-sm" align="start">
              <p>
                This is your numeric GitLab user ID, not your username. While logged in to your
                GitLab instance, open <span className="font-mono text-xs">/api/v4/user</span> — the
                JSON response&apos;s <span className="font-mono text-xs">id</span> field is it.
              </p>
            </PopoverContent>
          </Popover>
        </div>
        <div className="mt-1 flex gap-2">
          <Input
            id="gitlab-identity-user-id"
            type="number"
            inputMode="numeric"
            min={1}
            value={gitlabUserId}
            onChange={(e) => setGitlabUserId(e.target.value)}
          />
          <Button type="button" variant="outline" size="sm" disabled={!baseUrl.trim()} asChild={!!baseUrl.trim()}>
            {baseUrl.trim() ? (
              <a href={`${normalizeBaseUrl(baseUrl)}/api/v4/user`} target="_blank" rel="noopener noreferrer">
                Look up ID
                <SquareArrowOutUpRightIcon className="size-3.5" />
              </a>
            ) : (
              <>Look up ID</>
            )}
          </Button>
        </div>
      </div>
      <div>
        <label htmlFor="gitlab-identity-username" className="text-foreground block text-sm font-medium">
          GitLab username
        </label>
        <Input
          id="gitlab-identity-username"
          value={gitlabUsername}
          onChange={(e) => setGitlabUsername(e.target.value)}
          className="mt-1"
        />
      </div>
      <Button type="submit" size="sm" disabled={pending}>
        {pending ? "Saving…" : "Save"}
      </Button>
    </form>
  );
}

/**
 * GitlabIdentitySection lets the current user register their own GitLab
 * user ID/username per GitLab base URL (issue #102), so ?assignee=me on the
 * task collections can find tasks assigned to them. Rendered on /settings,
 * since it acts on the caller's own account, not a project.
 */
export function GitlabIdentitySection({ identities }: { identities: GitlabIdentity[] }) {
  const router = useRouter();

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">GitLab identity</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-muted-foreground text-sm">
          Register your GitLab user ID so tasks assigned to you can be filtered with &quot;assigned
          to me&quot;.
        </p>
        {identities.length > 0 ? (
          <ul className="space-y-2">
            {identities.map((identity) => (
              <li key={identity.id} className="border-border rounded-md border px-3 py-2 text-sm">
                <p className="text-foreground font-medium">{identity.gitlabBaseUrl}</p>
                <p className="text-muted-foreground text-xs">
                  {identity.gitlabUsername || "(no username)"} · user ID {identity.gitlabUserId}
                </p>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-muted-foreground text-sm">No GitLab identity registered yet.</p>
        )}
        <RegisterIdentityForm onSaved={() => router.refresh()} />
      </CardContent>
    </Card>
  );
}
