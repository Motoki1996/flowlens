"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type {
  ApiError,
  ProjectInvite,
  ProjectInviteWithToken,
  ProjectMemberRole,
} from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CreateFormRegion } from "@/components/CreateFormRegion";
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

/** inviteUrl builds the link handed to the person being invited. It is
 *  assembled in the browser, from this app's own origin, because the API
 *  has no idea what hostname a self-hoster serves FlowLens on. */
function inviteUrl(token: string) {
  return `${window.location.origin}/invites/${token}`;
}

/**
 * CreateInviteForm collects a role and expiry, then issues an invite.
 * onCreated receives the raw token — the only place it is ever available —
 * so the caller can show the resulting link once.
 */
function CreateInviteForm({
  projectId,
  onCreated,
  onCancel,
}: {
  projectId: string;
  onCreated: (invite: ProjectInviteWithToken) => void;
  onCancel: () => void;
}) {
  const router = useRouter();
  const [role, setRole] = useState<ProjectMemberRole>("member");
  const [expiresInDays, setExpiresInDays] = useState("7");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/invites`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ role, expiresInDays: Number(expiresInDays) || 0 }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to create the invite."));
        return;
      }
      const invite = (await res.json()) as ProjectInviteWithToken;
      router.refresh();
      onCreated(invite);
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="Create invite">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <fieldset className="space-y-2 text-sm">
        <legend className="text-foreground font-medium">Role</legend>
        {(["viewer", "member", "owner"] as const).map((r) => (
          <label key={r} className="flex items-center gap-2 capitalize">
            <input
              type="radio"
              name="invite-role"
              checked={role === r}
              onChange={() => setRole(r)}
            />
            {r}
          </label>
        ))}
      </fieldset>
      <div>
        <label htmlFor="invite-expires-in" className="text-foreground block text-sm font-medium">
          Expires in (days)
        </label>
        <Input
          id="invite-expires-in"
          type="number"
          min={1}
          max={90}
          value={expiresInDays}
          onChange={(e) => setExpiresInDays(e.target.value)}
          className="mt-1"
        />
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Creating…" : "Create invite"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/**
 * NewInviteModal shows the invite link exactly once. There is no way to
 * re-open it — the API stores only the token's hash, so once this is closed
 * the link is unrecoverable and a new invite has to be created.
 */
function NewInviteModal({ invite, onClose }: { invite: ProjectInviteWithToken; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const url = inviteUrl(invite.token);

  async function handleCopy() {
    await navigator.clipboard.writeText(url);
    setCopied(true);
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Invite created</DialogTitle>
          <DialogDescription>
            Send this link to the person you are inviting. It works once, expires{" "}
            {formatDateTime(invite.expiresAt)}, and will not be shown again. FlowLens sends no
            email.
          </DialogDescription>
        </DialogHeader>
        <div className="bg-muted rounded-md border px-3 py-2 font-mono text-sm break-all">
          {url}
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

/** RevokeInviteButton interposes an inline confirmation, matching the API
 *  token card: a revoked invite cannot be un-revoked. */
function RevokeInviteButton({ inviteId }: { inviteId: string }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleRevoke() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/invites/${inviteId}`, {
        method: "DELETE",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        setError(await parseError(res, "Failed to revoke the invite."));
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

/** InviteRow is one invite's summary. A spent or expired invite keeps its
 *  row rather than disappearing, so an owner can see who was let in. */
function InviteRow({ invite }: { invite: ProjectInvite }) {
  return (
    <li className="border-border rounded-md border px-3 py-2 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-foreground font-medium capitalize">{invite.role}</p>
          <p className="text-muted-foreground font-mono text-xs">{invite.tokenPrefix}…</p>
        </div>
        {invite.status === "pending" ? <RevokeInviteButton inviteId={invite.id} /> : null}
      </div>
      <div className="text-muted-foreground mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
        {invite.status === "accepted" ? (
          <Badge variant="secondary">Accepted</Badge>
        ) : invite.status === "expired" ? (
          <Badge variant="outline" className="border-destructive text-destructive">
            Expired
          </Badge>
        ) : (
          <Badge>Pending</Badge>
        )}
        <span>
          {invite.status === "accepted" && invite.acceptedAt
            ? `Accepted: ${formatDateTime(invite.acceptedAt)}`
            : `Expires: ${formatDateTime(invite.expiresAt)}`}
        </span>
      </div>
    </li>
  );
}

/**
 * ProjectInviteSection is the ProjectInvite collection, rendered as a
 * section inside the Project single view (docs/ui-design.md) rather than a
 * standalone screen — an invite only ever exists in the context of the
 * project it admits someone to.
 *
 * It is what lets an instance keep registration closed (ALLOW_SIGNUP=false)
 * and still add people: adding a member by username requires them to have an
 * account already, and without invites there is no way to get one.
 *
 * `invites` is null when the caller is not the project's owner — the listing
 * endpoint is owner-only — in which case the whole card is hidden rather
 * than shown as empty, since nothing in it would be actionable.
 */
export function ProjectInviteSection({
  projectId,
  invites,
}: {
  projectId: string;
  invites: ProjectInvite[] | null;
}) {
  const [creating, setCreating] = useState(false);
  const [createdInvite, setCreatedInvite] = useState<ProjectInviteWithToken | null>(null);

  if (invites === null) return null;

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Invites</CardTitle>
          {!creating ? (
            <Button variant="outline" size="sm" onClick={() => setCreating(true)}>
              Create invite
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-muted-foreground text-sm">
          An invite link lets someone with no FlowLens account create one and join this project —
          the way to add people while registration is closed. Each link works once.
        </p>
        {creating ? (
          <CreateFormRegion>
            <CreateInviteForm
              projectId={projectId}
              onCreated={(invite) => {
                setCreating(false);
                setCreatedInvite(invite);
              }}
              onCancel={() => setCreating(false)}
            />
          </CreateFormRegion>
        ) : null}
        {invites.length === 0 ? (
          <p className="text-muted-foreground text-sm">No invites created yet.</p>
        ) : (
          <ul className="space-y-2">
            {invites.map((invite) => (
              <InviteRow key={invite.id} invite={invite} />
            ))}
          </ul>
        )}
      </CardContent>
      {createdInvite ? (
        <NewInviteModal invite={createdInvite} onClose={() => setCreatedInvite(null)} />
      ) : null}
    </Card>
  );
}
