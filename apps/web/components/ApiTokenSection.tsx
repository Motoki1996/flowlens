"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError, ApiToken, ApiTokenScope, ApiTokenWithSecret } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

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

/** A token within this many milliseconds of its expiry (but not past it yet)
 *  is flagged "Expires soon" so it doesn't lapse as a silent surprise. */
const EXPIRING_SOON_MS = 7 * 24 * 60 * 60 * 1000;

function expiryStatus(expiresAt: string | null): "expired" | "soon" | null {
  if (!expiresAt) return null;
  const remaining = new Date(expiresAt).getTime() - Date.now();
  if (remaining <= 0) return "expired";
  if (remaining <= EXPIRING_SOON_MS) return "soon";
  return null;
}

/**
 * IssueTokenForm collects a name, scope and optional expiry, then issues a
 * new token (POST .../api-tokens). onIssued receives the raw bearer value —
 * the only place it is ever available — so the caller can show it once.
 */
function IssueTokenForm({
  projectId,
  onIssued,
  onCancel,
}: {
  projectId: string;
  onIssued: (token: ApiTokenWithSecret) => void;
  onCancel: () => void;
}) {
  const router = useRouter();
  const [name, setName] = useState("");
  const [scope, setScope] = useState<ApiTokenScope>("read");
  const [expiresOn, setExpiresOn] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setError("Name is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/api-tokens`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name,
          scopes: scope === "write" ? ["read", "write"] : ["read"],
          expiresAt: expiresOn ? new Date(expiresOn).toISOString() : null,
        }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to issue the token."));
        return;
      }
      const token = (await res.json()) as ApiTokenWithSecret;
      router.refresh();
      onIssued(token);
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="Issue API token">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="api-token-name" className="text-foreground block text-sm font-medium">
          Name
        </label>
        <Input
          id="api-token-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>
      <fieldset className="space-y-2 text-sm">
        <legend className="text-foreground font-medium">Scope</legend>
        <label className="flex items-center gap-2">
          <input
            type="radio"
            name="api-token-scope"
            checked={scope === "read"}
            onChange={() => setScope("read")}
          />
          Read-only
        </label>
        <label className="flex items-center gap-2">
          <input
            type="radio"
            name="api-token-scope"
            checked={scope === "write"}
            onChange={() => setScope("write")}
          />
          Read &amp; write
        </label>
      </fieldset>
      <div>
        <label htmlFor="api-token-expires" className="text-foreground block text-sm font-medium">
          Expires on (optional)
        </label>
        <Input
          id="api-token-expires"
          type="date"
          value={expiresOn}
          onChange={(e) => setExpiresOn(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Issuing…" : "Issue token"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/**
 * NewTokenModal shows a freshly issued token's raw value exactly once. It
 * carries no way to re-open — once closed (or navigated away from), the
 * value is gone for good, same as the API never returning it again.
 */
function NewTokenModal({ token, onClose }: { token: ApiTokenWithSecret; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    await navigator.clipboard.writeText(token.token);
    setCopied(true);
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Token issued</DialogTitle>
          <DialogDescription>
            Copy this token now. This value will not be shown again.
          </DialogDescription>
        </DialogHeader>
        <div className="bg-muted rounded-md border px-3 py-2 font-mono text-sm break-all">
          {token.token}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={handleCopy}>
            {copied ? "Copied" : "Copy"}
          </Button>
          <Button type="button" onClick={onClose}>
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** RevokeTokenButton interposes an inline confirmation before revoking — a
 *  revoked token cannot be un-revoked. */
function RevokeTokenButton({ tokenId }: { tokenId: string }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleRevoke() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/api-tokens/${tokenId}`, {
        method: "DELETE",
        credentials: "include",
      });
      if (!res.ok && res.status !== 204) {
        setError(await parseError(res, "Failed to revoke the token."));
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
        <div className="flex gap-2">
          <Button variant="destructive" size="sm" onClick={handleRevoke} disabled={pending}>
            {pending ? "Revoking…" : "Confirm revoke"}
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
      Revoke
    </Button>
  );
}

/** TokenRow is one issued token's summary: identity, scope, activity, and its revoke action. */
function TokenRow({ token }: { token: ApiToken }) {
  const status = expiryStatus(token.expiresAt);

  return (
    <li className="border-border rounded-md border px-3 py-2 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-foreground font-medium">{token.name}</p>
          <p className="text-muted-foreground font-mono text-xs">{token.tokenPrefix}…</p>
        </div>
        <RevokeTokenButton tokenId={token.id} />
      </div>
      <div className="text-muted-foreground mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
        <Badge variant={token.scopes.includes("write") ? "default" : "secondary"}>
          {token.scopes.includes("write") ? "Read & write" : "Read-only"}
        </Badge>
        {status === "expired" ? (
          <Badge variant="outline" className="border-destructive text-destructive">
            Expired
          </Badge>
        ) : status === "soon" ? (
          <Badge variant="outline">Expires soon</Badge>
        ) : null}
        <span>Last used: {token.lastUsedAt ? formatDateTime(token.lastUsedAt) : "Never"}</span>
        <span>Expires: {token.expiresAt ? formatDateTime(token.expiresAt) : "Never"}</span>
      </div>
    </li>
  );
}

/**
 * ApiTokenSection is the ApiToken collection, rendered as a section inside
 * the Project single view (docs/ui-design.md) rather than a standalone
 * screen — a token only ever exists in the context of the project it grants
 * access to. Issuing shows the raw bearer value exactly once, in
 * NewTokenModal; every other rendering (this list, the API response it came
 * from) only ever carries tokenPrefix.
 */
export function ApiTokenSection({ projectId, tokens }: { projectId: string; tokens: ApiToken[] }) {
  const [issuing, setIssuing] = useState(false);
  const [issuedToken, setIssuedToken] = useState<ApiTokenWithSecret | null>(null);

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">API tokens</CardTitle>
          {!issuing ? (
            <Button variant="outline" size="sm" onClick={() => setIssuing(true)}>
              Issue token
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {issuing ? (
          <IssueTokenForm
            projectId={projectId}
            onIssued={(token) => {
              setIssuing(false);
              setIssuedToken(token);
            }}
            onCancel={() => setIssuing(false)}
          />
        ) : null}
        {tokens.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            API tokens let an AI agent or other external tool read (and optionally write) this
            project&apos;s tasks without a user login. No tokens issued yet.
          </p>
        ) : (
          <ul className="space-y-2">
            {tokens.map((token) => (
              <TokenRow key={token.id} token={token} />
            ))}
          </ul>
        )}
      </CardContent>
      {issuedToken ? <NewTokenModal token={issuedToken} onClose={() => setIssuedToken(null)} /> : null}
    </Card>
  );
}
