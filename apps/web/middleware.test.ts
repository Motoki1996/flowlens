import { describe, it, expect } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "./middleware";

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
