import { describe, it, expect, vi, beforeEach } from "vitest";

// next/headers is server-only; mock it so cookies() works in the test env.
vi.mock("next/headers", () => ({
  cookies: async () => ({
    toString: () => "flowlens_session=abc",
  }),
}));

import { getCurrentUser } from "./api";

describe("getCurrentUser", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns the user on 200", async () => {
    const user = {
      id: "1",
      username: "octocat",
      email: "octocat@example.com",
      displayName: "The Octocat",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(user), { status: 200 })),
    );

    const result = await getCurrentUser();
    expect(result).toEqual(user);
  });

  it("returns null on 401", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("", { status: 401 })),
    );
    const result = await getCurrentUser();
    expect(result).toBeNull();
  });

  it("returns null when the API is unreachable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("ECONNREFUSED");
      }),
    );
    const result = await getCurrentUser();
    expect(result).toBeNull();
  });

  it("forwards the session cookie", async () => {
    const fetchMock = vi.fn(
      async (_url: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({}), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getCurrentUser();

    const [, init] = fetchMock.mock.calls[0];
    expect(init?.headers).toMatchObject({
      cookie: "flowlens_session=abc",
    });
  });
});
