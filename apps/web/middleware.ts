import { NextResponse, type NextRequest } from "next/server";

// Kept in sync with apps/api/internal/http/cookies.go's sessionCookieName.
const SESSION_COOKIE_NAME = "flowlens_session";

/**
 * Redirects to /login when the session cookie is entirely absent (issue #94).
 * This only checks *presence*, not validity — verifying the cookie means
 * asking the API, and doing that on every Edge request would double the
 * round trips a page already makes via getCurrentUser(). A present-but-expired
 * or otherwise invalid cookie passes through here; getCurrentUser() still
 * catches it (see the pages that keep their own `if (!user) redirect` for
 * that reason) and redirects from there instead.
 */
export function middleware(request: NextRequest) {
  const hasSession = request.cookies.has(SESSION_COOKIE_NAME);
  if (!hasSession) {
    const loginUrl = new URL("/login", request.url);
    return NextResponse.redirect(loginUrl);
  }
  return NextResponse.next();
}

export const config = {
  // Everything except the public auth screens, static assets, and Next.js
  // internals. The root "/" always redirects to /dashboard regardless of
  // auth, so it is left in scope on purpose.
  //
  // invites/ is excluded because the person it is for may have no account
  // at all — bouncing them to /login is precisely the dead end issue #211
  // exists to remove. The screen itself handles both the signed-in and the
  // signed-out case.
  //
  // api/auth/webhooks are excluded because they are not screens at all:
  // next.config.ts rewrites them to the Go API, which does its own
  // authentication and returns 401 to a caller without a session. Middleware
  // runs before rewrites, so leaving them in scope would bounce them to
  // /login instead — including POST /auth/login itself, which by definition
  // has no session cookie yet, and the GitLab webhook receiver, which never
  // has one.
  matcher: [
    "/((?!login|signup|invites/|api/|auth/|webhooks/|_next/static|_next/image|favicon.ico).*)",
  ],
};
