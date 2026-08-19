"use client";

import { useState, type FormEvent } from "react";
import { API_PUBLIC_URL } from "@/lib/config";
import { csrfHeaders } from "@/lib/csrf";
import type { ApiError } from "@/types";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const MIN_PASSWORD_LENGTH = 8;

/**
 * PasswordChangeSection lets the current user change their own password
 * (issue #210). Rendered on /settings alongside the account fields, since
 * it acts on the caller's own account rather than on a project.
 *
 * The API revokes every session on success and issues a fresh one in the
 * same response, so the browser stays logged in on a new cookie while any
 * other session — the reason someone changes a password in the first place
 * — is cut. Nothing needs reloading here as a result.
 */
export function PasswordChangeSection() {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSaved(false);

    if (newPassword.length < MIN_PASSWORD_LENGTH) {
      setError(`New password must be at least ${MIN_PASSWORD_LENGTH} characters.`);
      return;
    }
    // Checked here rather than server-side: the confirmation field exists
    // only to catch a typo in the new password, and the API never sees it.
    if (newPassword !== confirmPassword) {
      setError("New password and confirmation do not match.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/api/v1/me/password`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...csrfHeaders() },
        body: JSON.stringify({ currentPassword, newPassword }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Failed to change the password.");
        return;
      }
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setSaved(true);
    } finally {
      setPending(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Password</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-3" aria-label="Change password">
          <p className="text-muted-foreground text-sm">
            Changing your password signs out every other session. This one stays signed in.
          </p>
          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          {saved ? (
            <Alert>
              <AlertDescription>Password changed.</AlertDescription>
            </Alert>
          ) : null}
          <div>
            <label htmlFor="current-password" className="text-foreground block text-sm font-medium">
              Current password
            </label>
            <Input
              id="current-password"
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              className="mt-1"
            />
          </div>
          <div>
            <label htmlFor="new-password" className="text-foreground block text-sm font-medium">
              New password
            </label>
            <Input
              id="new-password"
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className="mt-1"
            />
            <p className="text-muted-foreground mt-1 text-xs">
              At least {MIN_PASSWORD_LENGTH} characters.
            </p>
          </div>
          <div>
            <label htmlFor="confirm-password" className="text-foreground block text-sm font-medium">
              Confirm new password
            </label>
            <Input
              id="confirm-password"
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="mt-1"
            />
          </div>
          <Button type="submit" size="sm" disabled={pending}>
            {pending ? "Changing…" : "Change password"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
