"use client";

import { useEffect, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type {
  ApiError,
  ProjectMember,
  ProjectMemberCandidate,
  ProjectMemberRole,
} from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CreateFormRegion } from "@/components/CreateFormRegion";
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

/** The API ignores anything shorter, so don't spend a request on it. */
const CANDIDATE_MIN_QUERY = 2;
/** Long enough that typing a name is one request, not one per keystroke. */
const CANDIDATE_DEBOUNCE_MS = 250;

/**
 * useMemberCandidates searches the people the owner could invite as they
 * type, debounced, with the in-flight request aborted whenever the term
 * changes. A failure is reported rather than thrown: the field still accepts
 * an exact username or email, which is the only way to invite someone the
 * search deliberately cannot see (issue #140).
 */
function useMemberCandidates(projectId: string, query: string) {
  const [candidates, setCandidates] = useState<ProjectMemberCandidate[]>([]);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const term = query.trim();
    if (term.length < CANDIDATE_MIN_QUERY) {
      setCandidates([]);
      setFailed(false);
      return;
    }

    const controller = new AbortController();
    const timer = setTimeout(async () => {
      try {
        const res = await fetch(
          `${API_PUBLIC_URL}/api/v1/projects/${projectId}/member-candidates` +
            `?q=${encodeURIComponent(term)}`,
          { credentials: "include", signal: controller.signal },
        );
        if (!res.ok) throw new Error(`search failed: ${res.status}`);
        setCandidates((await res.json()) as ProjectMemberCandidate[]);
        setFailed(false);
      } catch {
        if (controller.signal.aborted) return;
        setCandidates([]);
        setFailed(true);
      }
    }, CANDIDATE_DEBOUNCE_MS);

    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [projectId, query]);

  return { candidates, failed };
}

/**
 * IdentifierCombobox is the invite form's identifier field: a plain text
 * input that also suggests matching users below it. It follows the ARIA
 * combobox-with-listbox pattern by hand rather than reusing ui/combobox,
 * which selects from a fixed client-side list — here the options arrive
 * asynchronously and, crucially, typing something no option matches is still
 * a valid entry.
 */
function IdentifierCombobox({
  projectId,
  value,
  onChange,
  disabled,
}: {
  projectId: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}) {
  const { candidates, failed } = useMemberCandidates(projectId, value);
  const [dismissed, setDismissed] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const open = candidates.length > 0 && !dismissed;

  function select(candidate: ProjectMemberCandidate) {
    onChange(candidate.username);
    setDismissed(true);
    setActiveIndex(-1);
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Escape") {
      setDismissed(true);
      setActiveIndex(-1);
      return;
    }
    if (!open) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => (i + 1) % candidates.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => (i <= 0 ? candidates.length - 1 : i - 1));
    } else if (e.key === "Enter" && activeIndex >= 0) {
      // Only swallow Enter when a suggestion is highlighted; otherwise it
      // submits the form with whatever was typed, as it always did.
      e.preventDefault();
      select(candidates[activeIndex]);
    }
  }

  return (
    <div className="relative">
      <Input
        id="member-identifier"
        role="combobox"
        aria-expanded={open}
        aria-controls="member-candidates"
        aria-autocomplete="list"
        aria-activedescendant={
          open && activeIndex >= 0 ? `member-candidate-${activeIndex}` : undefined
        }
        autoComplete="off"
        value={value}
        disabled={disabled}
        onChange={(e) => {
          onChange(e.target.value);
          setDismissed(false);
          setActiveIndex(-1);
        }}
        onKeyDown={handleKeyDown}
        className="mt-1"
      />
      {open ? (
        <ul
          id="member-candidates"
          role="listbox"
          aria-label="Matching users"
          className="bg-popover border-border absolute z-10 mt-1 w-full overflow-hidden rounded-md border shadow-md"
        >
          {candidates.map((candidate, index) => (
            <li
              key={candidate.userId}
              id={`member-candidate-${index}`}
              role="option"
              aria-selected={index === activeIndex}
              // mousedown, not click: the input must not lose focus first.
              onMouseDown={(e) => {
                e.preventDefault();
                select(candidate);
              }}
              onMouseEnter={() => setActiveIndex(index)}
              className={`cursor-pointer px-3 py-2 text-sm ${
                index === activeIndex ? "bg-accent text-accent-foreground" : ""
              }`}
            >
              <span className="font-medium">{candidate.displayName}</span>{" "}
              <span className="text-muted-foreground">@{candidate.username}</span>
            </li>
          ))}
        </ul>
      ) : null}
      <p className="text-muted-foreground mt-1 text-xs" role={failed ? "status" : undefined}>
        {failed
          ? "Couldn't load suggestions — enter an exact username or email instead."
          : "Suggests people you already share a project with. Anyone else can still be " +
            "invited by their exact username or email."}
      </p>
    </div>
  );
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
        <IdentifierCombobox
          projectId={projectId}
          value={identifier}
          onChange={setIdentifier}
          disabled={pending}
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

/** MemberRow is one member's summary. The role-change and remove controls are
 *  shown only when the section is editable (i.e. the caller is an owner) *and*
 *  the row is one the API would actually let them act on: never their own row
 *  and never the project's designated owner's (issue #139). Both of those are
 *  rejected server-side, so offering the control would only ever produce an
 *  error after the fact. */
function MemberRow({
  projectId,
  member,
  editable,
  isSelf,
}: {
  projectId: string;
  member: ProjectMember;
  editable: boolean;
  isSelf: boolean;
}) {
  const actionable = editable && !isSelf && !member.isProjectOwner;
  return (
    <li className="border-border rounded-md border px-3 py-2 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-foreground font-medium">
            {member.displayName}
            {isSelf ? (
              <Badge variant="outline" className="ml-2 align-middle">
                You
              </Badge>
            ) : null}
          </p>
          <p className="text-muted-foreground text-xs">
            @{member.username} · Member since {formatDate(member.createdAt)}
          </p>
        </div>
        {actionable ? (
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
 *
 * currentUserId identifies the viewer's own row, which carries no controls —
 * this section manages *other* people's access (issue #139).
 */
export function ProjectMemberSection({
  projectId,
  members,
  currentUserId,
}: {
  projectId: string;
  members: ProjectMember[] | null;
  currentUserId: string;
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
              <CreateFormRegion>
                <AddMemberForm
                  projectId={projectId}
                  onAdded={() => {
                    setAdding(false);
                    router.refresh();
                  }}
                  onCancel={() => setAdding(false)}
                />
              </CreateFormRegion>
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
                    isSelf={member.userId === currentUserId}
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
