// Client-safe configuration. Values here must be usable in both server
// and client components, so this module must not import server-only APIs.

// Base URL the browser uses to reach the API.
//
// It defaults to the empty string, which makes every call relative and so
// same-origin: the browser talks to the Next.js server, whose rewrites (see
// next.config.ts) proxy /api/* and /auth/* through to the API. That default
// is what lets one prebuilt web image serve any hostname — NEXT_PUBLIC_*
// values are inlined into the client bundle at build time, so a baked-in
// absolute URL could not be changed by a self-hoster without rebuilding.
//
// Setting NEXT_PUBLIC_API_BASE_URL is still supported for a deployment that
// puts the API on its own origin, but then the API's CORS origin
// (WEB_BASE_URL) must match, and the session cookie is SameSite=Lax — so
// the two origins have to stay on one registrable domain.
export const API_PUBLIC_URL = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
