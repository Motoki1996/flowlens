import { describe, expect, it, vi } from "vitest";
import { mkdtemp, readFile, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { runInit } from "../src/init.js";

async function makeTmpDir() {
  return mkdtemp(join(tmpdir(), "agent-kit-test-"));
}

function fakeFetch(spec = "openapi: 3.1.0\ninfo:\n  title: FlowLens\n", projectId = "proj-123") {
  return vi.fn(async (url) => {
    if (url.endsWith("/api/v1/projects")) {
      return {
        ok: true,
        status: 200,
        statusText: "OK",
        json: async () => [{ id: projectId }],
      };
    }
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      text: async () => spec,
    };
  });
}

describe("runInit", () => {
  it("writes the skill, commands, spec and config when a token is set and the instance is reachable", async () => {
    const cwd = await makeTmpDir();
    const fetchImpl = fakeFetch();

    const { results, warnings } = await runInit({
      url: "https://flowlens.example.com/",
      cwd,
      fetchImpl,
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    expect(warnings).toEqual([]);
    expect(fetchImpl).toHaveBeenCalledWith(
      "https://flowlens.example.com/api/v1/projects",
      expect.objectContaining({ headers: { Authorization: "Bearer flt_test" } }),
    );
    expect(fetchImpl).toHaveBeenCalledWith("https://flowlens.example.com/openapi.yaml");

    const config = JSON.parse(await readFile(join(cwd, ".flowlens/config.json"), "utf8"));
    expect(config.baseUrl).toBe("https://flowlens.example.com");
    expect(config.projectId).toBe("proj-123");

    const spec = await readFile(join(cwd, ".flowlens/openapi.yaml"), "utf8");
    expect(spec).toContain("FlowLens");

    const skill = await readFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "utf8");
    expect(skill).toContain("name: flowlens");

    for (const cmd of ["refine-backlog", "breakdown-epics", "breakdown", "work"]) {
      const content = await readFile(join(cwd, `.claude/commands/flowlens/${cmd}.md`), "utf8");
      expect(content.length).toBeGreaterThan(0);
    }

    expect(results.find((r) => r.path === ".claude/skills/flowlens/SKILL.md").status).toBe("created");
  });

  it("still installs the skill/commands, but skips .flowlens/, when no token is set", async () => {
    const cwd = await makeTmpDir();
    const fetchImpl = fakeFetch();

    const { results, warnings } = await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl,
      env: {},
    });

    expect(fetchImpl).not.toHaveBeenCalled();
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toMatch(/FLOWLENS_API_TOKEN/);

    expect(results.find((r) => r.path === ".claude/skills/flowlens/SKILL.md").status).toBe("created");
    expect(results.some((r) => r.path.startsWith(".flowlens/"))).toBe(false);
  });

  it("still installs the skill/commands, but skips .flowlens/, when the instance isn't reachable", async () => {
    const cwd = await makeTmpDir();
    const fetchImpl = vi.fn(async () => {
      throw new Error("fetch failed");
    });

    const { results, warnings } = await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl,
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toMatch(/could not reach/);
    expect(results.find((r) => r.path === ".claude/skills/flowlens/SKILL.md").status).toBe("created");
    expect(results.some((r) => r.path.startsWith(".flowlens/"))).toBe(false);
  });

  it("does not overwrite existing skill/command files without --force", async () => {
    const cwd = await makeTmpDir();
    await mkdir(join(cwd, ".claude/skills/flowlens"), { recursive: true });
    await writeFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "custom content\n", "utf8");

    const { results } = await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl: fakeFetch(),
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    const skillResult = results.find((r) => r.path === ".claude/skills/flowlens/SKILL.md");
    expect(skillResult.status).toBe("skipped");

    const skill = await readFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "utf8");
    expect(skill).toBe("custom content\n");
  });

  it("overwrites existing skill/command files with --force", async () => {
    const cwd = await makeTmpDir();
    await mkdir(join(cwd, ".claude/skills/flowlens"), { recursive: true });
    await writeFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "custom content\n", "utf8");

    const { results } = await runInit({
      url: "https://flowlens.example.com",
      force: true,
      cwd,
      fetchImpl: fakeFetch(),
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    const skillResult = results.find((r) => r.path === ".claude/skills/flowlens/SKILL.md");
    expect(skillResult.status).toBe("overwritten");

    const skill = await readFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "utf8");
    expect(skill).toContain("name: flowlens");
  });

  it("always refreshes the spec and config even without --force", async () => {
    const cwd = await makeTmpDir();
    await mkdir(join(cwd, ".flowlens"), { recursive: true });
    await writeFile(join(cwd, ".flowlens/openapi.yaml"), "stale\n", "utf8");
    await writeFile(join(cwd, ".flowlens/config.json"), "{}", "utf8");

    const { results } = await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl: fakeFetch("openapi: 3.1.0\nfresh: true\n"),
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    expect(results.find((r) => r.path === ".flowlens/openapi.yaml").status).toBe("overwritten");
    expect(results.find((r) => r.path === ".flowlens/config.json").status).toBe("overwritten");

    const spec = await readFile(join(cwd, ".flowlens/openapi.yaml"), "utf8");
    expect(spec).toContain("fresh: true");
  });

  it("removes a stale .flowlens/ gitignore entry left by an older agent-kit", async () => {
    const cwd = await makeTmpDir();
    await writeFile(join(cwd, ".gitignore"), "node_modules/\n.flowlens/\n", "utf8");

    await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl: fakeFetch(),
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    const gitignore = await readFile(join(cwd, ".gitignore"), "utf8");
    expect(gitignore).not.toContain(".flowlens/");
    expect(gitignore).toContain("node_modules/");
  });

  it("leaves .gitignore untouched when there's no .flowlens/ entry to remove", async () => {
    const cwd = await makeTmpDir();

    const { results } = await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl: fakeFetch(),
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    expect(results.some((r) => r.path === ".gitignore")).toBe(false);
  });

  it("throws when the token doesn't resolve to exactly one project", async () => {
    const cwd = await makeTmpDir();
    const fetchImpl = vi.fn(async () => ({
      ok: true,
      status: 200,
      statusText: "OK",
      json: async () => [],
    }));

    const { warnings } = await runInit({
      url: "https://flowlens.example.com",
      cwd,
      fetchImpl,
      env: { FLOWLENS_API_TOKEN: "flt_test" },
    });

    expect(warnings[0]).toMatch(/exactly one project/);
  });

  it("requires url", async () => {
    await expect(runInit({})).rejects.toThrow(/--url/);
  });
});
