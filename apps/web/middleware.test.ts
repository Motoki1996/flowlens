import { describe, it, expect } from "vitest";
import { NextRequest } from "next/server";
import { middleware, config } from "./middleware";

function requestFor(path: string, cookie?: string) {
  const headers = cookie ? { cookie } : undefined;
  return new NextRequest(new URL(path, "http://localhost:3000"), { headers });
}

describe("middleware", () => {
  it("redirects to /login when the session cookie is absent", () => {
    const res = middleware(requestFor("/dashboard"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/login");
  });

  it("passes through when the session cookie is present", () => {
    const res = middleware(requestFor("/dashboard", "flowlens_session=abc"));
    expect(res.status).toBe(200);
    expect(res.headers.get("location")).toBeNull();
  });

  it("passes through for an empty/expired-looking cookie value too", () => {
    // Presence, not validity, is all the middleware checks — getCurrentUser()
    // is the one that rejects an invalid token.
    const res = middleware(requestFor("/dashboard", "flowlens_session="));
    expect(res.status).toBe(200);
  });
});

// The matcher decides what the redirect above can reach, and getting it
// wrong is invisible to the tests above: they call middleware() directly.
// The paths next.config.ts rewrites to the Go API must stay out of scope —
// middleware runs before rewrites, so including them would bounce
// POST /auth/login (which has no session cookie yet, by definition) and the
// GitLab webhook receiver (which never has one) to /login.
describe("middleware matcher", () => {
  const matches = (path: string) =>
    config.matcher.some((pattern) => new RegExp(`^${pattern}$`).test(path));

  it.each([
    "/api/v1/me",
    "/api/v1/projects/p1/tasks",
    "/auth/login",
    "/auth/signup",
    "/auth/logout",
    "/webhooks/gitlab/abc",
    "/login",
    "/signup",
    "/_next/static/chunk.js",
  ])("leaves %s to be handled without an auth redirect", (path) => {
    expect(matches(path)).toBe(false);
  });

  it.each(["/", "/dashboard", "/tasks", "/projects/p1/merge-requests", "/settings"])(
    "guards the %s screen",
    (path) => {
      expect(matches(path)).toBe(true);
    },
  );
});
