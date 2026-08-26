import { describe, it, expect, vi, beforeEach } from "vitest";

// next/headers is server-only; mock it so cookies() works in the test env.
vi.mock("next/headers", () => ({
  cookies: async () => ({
    toString: () => "flowlens_session=abc",
  }),
}));

// The 401 answer is a redirect to /login, so the readers need next/navigation
// too. redirect() throws in Next itself (that is how it unwinds the render);
// the mock does the same so a caller can't accidentally carry on past it.
const redirectMock = vi.fn((path: string) => {
  throw new Error(`NEXT_REDIRECT:${path}`);
});
vi.mock("next/navigation", () => ({
  redirect: (path: string) => redirectMock(path),
}));

import { getBacklogs, getCurrentUser, getProject, getTask, getTaskDependencies, getTasks } from "./api";

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
    const fetchMock = vi.fn<typeof fetch>(
      async () => new Response(JSON.stringify({}), { status: 200 }),
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

  it("sends only the filters it was given, as the API's own parameter names", async () => {
    const fetchMock = vi.fn(async () => new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await getTasks("p1", { backlogId: "unassigned", status: "open", q: "login" });

    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    // backlog_id is the one snake_case query parameter in the API, and an
    // omitted filter is absent rather than empty.
    expect(url).toBe(
      "http://localhost:8080/api/v1/projects/p1/tasks?backlog_id=unassigned&status=open&q=login",
    );
  });

  it("sends ?assignee=me (issue #146)", async () => {
    const fetchMock = vi.fn(async () => new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await getTasks("p1", { assignee: "me" });

    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe("http://localhost:8080/api/v1/projects/p1/tasks?assignee=me");
  });

  it("requests the unfiltered list when given no filter", async () => {
    const fetchMock = vi.fn(async () => new Response("[]", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await getTasks("p2");

    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toBe("http://localhost:8080/api/v1/projects/p2/tasks");
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

/**
 * Every reader but getCurrentUser answers a 401 by redirecting to /login
 * rather than throwing "Failed to load …" (apiFetch). A page and the layout
 * above it render at the same time, so the layout's own redirect can't be
 * relied on to have happened first.
 */
describe("401 handling", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    redirectMock.mockClear();
  });

  it("redirects to /login instead of throwing a load failure", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));

    await expect(getProject("p1")).rejects.toThrow("NEXT_REDIRECT:/login");
    expect(redirectMock).toHaveBeenCalledWith("/login");
  });

  it("does the same for a collection reader", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));

    await expect(getBacklogs("p1")).rejects.toThrow("NEXT_REDIRECT:/login");
    expect(redirectMock).toHaveBeenCalledWith("/login");
  });

  it("leaves every other failure throwing, so it still reads as an API failure", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 500 })));

    await expect(getProject("p1")).rejects.toThrow("Failed to load project: 500");
    expect(redirectMock).not.toHaveBeenCalled();
  });

  it("still lets getCurrentUser report a signed-out visitor as null", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 401 })));

    await expect(getCurrentUser()).resolves.toBeNull();
    expect(redirectMock).not.toHaveBeenCalled();
  });
});
