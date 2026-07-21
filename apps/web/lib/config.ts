// Client-safe configuration. Values here must be usable in both server
// and client components, so this module must not import server-only APIs.

// Base URL the browser uses to reach the API (login/logout navigation).
export const API_PUBLIC_URL =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";
