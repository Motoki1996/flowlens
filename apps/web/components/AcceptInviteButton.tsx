"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

/**
 * AcceptInviteButton joins the signed-in user to the invite's project. It is
 * the half of /invites/[token] for someone who already has an account —
 * someone who does not sends the same token with their signup instead.
 */
export function AcceptInviteButton({ token, projectId }: { token: string; projectId: string }) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleAccept() {
    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/invites/accept`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ token }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to join the project.");
        setPending(false);
        return;
      }
      router.push(`/projects/${projectId}`);
      router.refresh();
    } catch {
      setError("Failed to join the project.");
      setPending(false);
    }
  }

  return (
    <div className="space-y-3">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <Button onClick={handleAccept} disabled={pending} className="w-full">
        {pending ? "Joining…" : "Join project"}
      </Button>
    </div>
  );
}
