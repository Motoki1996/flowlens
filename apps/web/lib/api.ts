// Server-side API client. Server Components call the Go API through the
// internal URL (the compose service name in Docker) and forward the
// browser's session cookie so the API can authenticate the request.

import { cookies } from "next/headers";
import type { User } from "@/types";

// Base URL the Next.js server uses to reach the API.
const API_INTERNAL_URL =
  process.env.API_INTERNAL_URL ?? "http://localhost:8080";

// Re-exported for server components that also need the public URL.
export { API_PUBLIC_URL } from "./config";

/**
 * getCurrentUser returns the authenticated user, or null when the request
 * has no valid session. It never throws for the unauthenticated case so
 * callers can redirect cleanly.
 */
export async function getCurrentUser(): Promise<User | null> {
  const cookieStore = await cookies();
  const cookieHeader = cookieStore.toString();

  let res: Response;
  try {
    res = await fetch(`${API_INTERNAL_URL}/api/v1/me`, {
      headers: { cookie: cookieHeader },
      // Always hit the API; the user session must not be cached.
      cache: "no-store",
    });
  } catch {
    // API unreachable — treat as signed out at the page level.
    return null;
  }

  if (res.status === 401) return null;
  if (!res.ok) {
    throw new Error(`Failed to load current user: ${res.status}`);
  }
  return (await res.json()) as User;
}
