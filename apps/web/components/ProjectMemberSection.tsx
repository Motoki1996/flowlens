"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError, ProjectMember, ProjectMemberRole } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const ROLES: { value: ProjectMemberRole; label: string }[] = [
  { value: "owner", label: "Owner" },
  { value: "member", label: "Member" },
  { value: "viewer", label: "Viewer" },
];

function roleLabel(role: ProjectMemberRole) {
  return ROLES.find((r) => r.value === role)?.label ?? role;
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

async function parseError(res: Response, fallback: string) {
  const body = (await res.json().catch(() => null)) as ApiError | null;
  return body?.error.message ?? fallback;
}

/**
 * AddMemberForm invites an existing user (by username or email) with a role,
 * mirroring ApiTokenSection's IssueTokenForm.
 */
function AddMemberForm({
  projectId,
  onAdded,
  onCancel,
}: {
  projectId: string;
  onAdded: () => void;
  onCancel: () => void;
}) {
  const [identifier, setIdentifier] = useState("");
  const [role, setRole] = useState<ProjectMemberRole>("member");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!identifier.trim()) {
      setError("Username or email is required.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/members`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ identifier, role }),
      });
      if (!res.ok) {
        setError(await parseError(res, "Failed to add the member."));
        return;
      }
      onAdded();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3" aria-label="Add member">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div>
        <label htmlFor="member-identifier" className="text-foreground block text-sm font-medium">
          Username or email
        </label>
        <Input
          id="member-identifier"
          value={identifier}
          onChange={(e) => setIdentifier(e.target.value)}
          className="mt-1"
        />
      </div>
      <div>
        <label htmlFor="member-role" className="text-foreground block text-sm font-medium">
          Role
        </label>
        <Select value={role} onValueChange={(value) => setRole(value as ProjectMemberRole)}>
          <SelectTrigger id="member-role" className="mt-1 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ROLES.map((r) => (
              <SelectItem key={r.value} value={r.value}>
                {r.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {pending ? "Adding…" : "Add member"}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

/** RemoveMemberButton interposes an inline confirmation before removing. */
function RemoveMemberButton({ projectId, userId }: { projectId: string; userId: string }) {
  const router = useRouter();
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleRemove() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/projects/${projectId}/members/${userId}`, {
        method: "DELETE",
        credentials: "include",
        headers: csrfHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        setError(await parseError(res, "Failed to remove the member."));
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
          <Button variant="destructive" size="sm" onClick={handleRemove} disabled={pending}>
            {pending ? "Removing…" : "Confirm remove"}
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
      Remove
    </Button>
  );
}

/** RoleSelect changes a member's role in place, showing a server error inline
 *  (e.g. trying to demote the project's designated owner). */
function RoleSelect({ projectId, member }: { projectId: string; member: ProjectMember }) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleChange(role: string) {
    if (role === member.role) return;
    setPending(true);
    setError(null);
    try {
      const res = await fetch(
        `${API_PUBLIC_URL}/api/v1/projects/${projectId}/members/${member.userId}`,
        {
          method: "PATCH",
          credentials: "include",
          headers: { "Content-Type": "application/json", ...csrfHeaders() },
          body: JSON.stringify({ role }),
        },
      );
      if (!res.ok) {
        setError(await parseError(res, "Failed to change the role."));
        return;
      }
      router.refresh();
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <Select value={member.role} onValueChange={handleChange} disabled={pending}>
        <SelectTrigger size="sm" aria-label={`Role for ${member.username}`}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {ROLES.map((r) => (
            <SelectItem key={r.value} value={r.value}>
              {r.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {error ? <span className="text-destructive text-xs">{error}</span> : null}
    </div>
  );
}

/** MemberRow is one member's summary, with role-change and remove controls
 *  shown only when the section is editable (i.e. the caller is an owner). */
function MemberRow({
  projectId,
  member,
  editable,
}: {
  projectId: string;
  member: ProjectMember;
  editable: boolean;
}) {
  return (
    <li className="border-border rounded-md border px-3 py-2 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-foreground font-medium">{member.displayName}</p>
          <p className="text-muted-foreground text-xs">
            @{member.username} · Member since {formatDate(member.createdAt)}
          </p>
        </div>
        {editable ? (
          <div className="flex items-start gap-2">
            <RoleSelect projectId={projectId} member={member} />
            <RemoveMemberButton projectId={projectId} userId={member.userId} />
          </div>
        ) : (
          <Badge variant={member.role === "owner" ? "default" : "secondary"}>
            {roleLabel(member.role)}
          </Badge>
        )}
      </div>
    </li>
  );
}

/**
 * ProjectMemberSection is the ProjectMember collection, rendered as a section
 * inside the Project single view (docs/ui-design.md) rather than a
 * standalone "manage members" screen, per issue #101. Managing membership is
 * owner-only (issue #100): `members` is `null` when the caller isn't an
 * owner (the listing endpoint itself 403s them), in which case the section
 * renders read-only with no way to tell who the members are.
 */
export function ProjectMemberSection({
  projectId,
  members,
}: {
  projectId: string;
  members: ProjectMember[] | null;
}) {
  const router = useRouter();
  const [adding, setAdding] = useState(false);
  const editable = members !== null;

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CardTitle className="text-base font-medium">Members</CardTitle>
          {editable && !adding ? (
            <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
              Add member
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {!editable ? (
          <p className="text-muted-foreground text-sm">
            Only the project&apos;s owners can view and manage its members.
          </p>
        ) : (
          <>
            {adding ? (
              <AddMemberForm
                projectId={projectId}
                onAdded={() => {
                  setAdding(false);
                  router.refresh();
                }}
                onCancel={() => setAdding(false)}
              />
            ) : null}
            {members.length === 0 ? (
              <p className="text-muted-foreground text-sm">No members yet.</p>
            ) : (
              <ul className="space-y-2">
                {members.map((member) => (
                  <MemberRow
                    key={member.userId}
                    projectId={projectId}
                    member={member}
                    editable={editable}
                  />
                ))}
              </ul>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
