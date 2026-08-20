"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { API_PUBLIC_URL } from "@/lib/config";
import type { ApiError } from "@/types";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { PasswordField } from "@/components/PasswordField";

const MIN_PASSWORD_LENGTH = 8;

/**
 * SignupForm creates a local account. `inviteToken`, when given, is sent
 * along with it: the API then exempts this signup from ALLOW_SIGNUP and
 * joins the new account to the invite's project (issue #211), which is what
 * the /invites/[token] screen uses it for.
 */
export function SignupForm({
  inviteToken,
  submitLabel = "Create account",
}: {
  inviteToken?: string;
  submitLabel?: string;
} = {}) {
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();

    // Checked here rather than server-side: the confirmation field exists
    // only to catch a typo, and the API never sees it. Same rule as
    // PasswordChangeSection.
    if (password !== confirmPassword) {
      setError("Password and confirmation do not match.");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const res = await fetch(`${API_PUBLIC_URL}/auth/signup`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(inviteToken ? { username, email, password, inviteToken } : { username, email, password }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as ApiError | null;
        setError(body?.error.message ?? "Sign-up failed.");
        return;
      }
      router.push("/dashboard");
      router.refresh();
    } finally {
      setPending(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="mt-6 space-y-4">
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <div>
        <label htmlFor="username" className="text-foreground block text-sm font-medium">
          Username
        </label>
        <input
          id="username"
          name="username"
          type="text"
          required
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          className="border-input bg-input/30 text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 mt-1 block w-full rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-[3px]"
        />
      </div>

      <div>
        <label htmlFor="email" className="text-foreground block text-sm font-medium">
          Email
        </label>
        <input
          id="email"
          name="email"
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className="border-input bg-input/30 text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 mt-1 block w-full rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-[3px]"
        />
      </div>

      <PasswordField
        id="password"
        label="Password"
        autoComplete="new-password"
        required
        minLength={MIN_PASSWORD_LENGTH}
        value={password}
        onChange={setPassword}
        hint={`At least ${MIN_PASSWORD_LENGTH} characters.`}
      />

      <PasswordField
        id="confirm-password"
        label="Confirm password"
        autoComplete="new-password"
        required
        minLength={MIN_PASSWORD_LENGTH}
        value={confirmPassword}
        onChange={setConfirmPassword}
      />

      <Button type="submit" disabled={pending} className="w-full">
        {pending ? "Creating account…" : submitLabel}
      </Button>
    </form>
  );
}
