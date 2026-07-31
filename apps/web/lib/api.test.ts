import { describe, it, expect, vi, beforeEach } from "vitest";

// next/headers is server-only; mock it so cookies() works in the test env.
vi.mock("next/headers", () => ({
  cookies: async () => ({
    toString: () => "flowlens_session=abc",
  }),
}));

import { getBacklogs, getCurrentUser, getTask, getTaskDependencies, getTasks } from "./api";

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

describe("getTasks", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns the project's tasks on 200", async () => {
    const tasks = [{ id: "t1", title: "Fix the bug" }];
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(tasks), { status: 200 })),
    );
    const result = await getTasks("p1");
    expect(result).toEqual(tasks);
  });

  it("throws on a non-2xx response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 500 })));
    await expect(getTasks("p1")).rejects.toThrow("Failed to load tasks: 500");
  });
});

describe("getBacklogs", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns the project's backlogs on 200", async () => {
    const backlogs = [{ id: "b1", name: "Sprint 1" }];
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(backlogs), { status: 200 })),
    );
    const result = await getBacklogs("p1");
    expect(result).toEqual(backlogs);
  });
});

describe("getTaskDependencies", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns the project's task dependencies on 200", async () => {
    const deps = [{ id: "d1", predecessorTaskId: "t1", successorTaskId: "t2" }];
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(deps), { status: 200 })),
    );
    const result = await getTaskDependencies("p1");
    expect(result).toEqual(deps);
  });

  it("throws on a non-2xx response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 500 })));
    await expect(getTaskDependencies("p1")).rejects.toThrow("Failed to load task dependencies: 500");
  });
});

describe("getTask", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns the task on 200", async () => {
    const task = { id: "t1", title: "Fix the bug" };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(task), { status: 200 })),
    );
    const result = await getTask("t1");
    expect(result).toEqual(task);
  });

  it("returns null on 404", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 404 })));
    const result = await getTask("unknown");
    expect(result).toBeNull();
  });
});
