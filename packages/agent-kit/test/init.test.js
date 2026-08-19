import { describe, expect, it, vi } from "vitest";
import { mkdtemp, readFile, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { runInit } from "../src/init.js";

async function makeTmpDir() {
  return mkdtemp(join(tmpdir(), "agent-kit-test-"));
}

function fakeFetch(spec = "openapi: 3.1.0\ninfo:\n  title: FlowLens\n") {
  return vi.fn(async () => ({
    ok: true,
    status: 200,
    statusText: "OK",
    text: async () => spec,
  }));
}

describe("runInit", () => {
  it("writes the spec, config, skill, commands, and gitignore entry", async () => {
    const cwd = await makeTmpDir();
    const fetchImpl = fakeFetch();

    const results = await runInit({
      url: "https://flowlens.example.com/",
      project: "proj-123",
      cwd,
      fetchImpl,
    });

    expect(fetchImpl).toHaveBeenCalledWith("https://flowlens.example.com/openapi.yaml");

    const config = JSON.parse(await readFile(join(cwd, ".flowlens/config.json"), "utf8"));
    expect(config.baseUrl).toBe("https://flowlens.example.com");
    expect(config.projectId).toBe("proj-123");

    const spec = await readFile(join(cwd, ".flowlens/openapi.yaml"), "utf8");
    expect(spec).toContain("FlowLens");

    const skill = await readFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "utf8");
    expect(skill).toContain("name: flowlens");

    for (const cmd of ["refine-backlog", "breakdown", "work"]) {
      const content = await readFile(join(cwd, `.claude/commands/flowlens/${cmd}.md`), "utf8");
      expect(content.length).toBeGreaterThan(0);
    }

    const gitignore = await readFile(join(cwd, ".gitignore"), "utf8");
    expect(gitignore).toContain(".flowlens/");

    expect(results.find((r) => r.path === ".claude/skills/flowlens/SKILL.md").status).toBe("created");
  });

  it("does not overwrite existing skill/command files without --force", async () => {
    const cwd = await makeTmpDir();
    await mkdir(join(cwd, ".claude/skills/flowlens"), { recursive: true });
    await writeFile(join(cwd, ".claude/skills/flowlens/SKILL.md"), "custom content\n", "utf8");

    const results = await runInit({
      url: "https://flowlens.example.com",
      project: "proj-123",
      cwd,
      fetchImpl: fakeFetch(),
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

    const results = await runInit({
      url: "https://flowlens.example.com",
      project: "proj-123",
      force: true,
      cwd,
      fetchImpl: fakeFetch(),
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

    const results = await runInit({
      url: "https://flowlens.example.com",
      project: "proj-123",
      cwd,
      fetchImpl: fakeFetch("openapi: 3.1.0\nfresh: true\n"),
    });

    expect(results.find((r) => r.path === ".flowlens/openapi.yaml").status).toBe("overwritten");
    expect(results.find((r) => r.path === ".flowlens/config.json").status).toBe("overwritten");

    const spec = await readFile(join(cwd, ".flowlens/openapi.yaml"), "utf8");
    expect(spec).toContain("fresh: true");
  });

  it("is idempotent about the .gitignore entry", async () => {
    const cwd = await makeTmpDir();
    await writeFile(join(cwd, ".gitignore"), "node_modules/\n.flowlens/\n", "utf8");

    await runInit({
      url: "https://flowlens.example.com",
      project: "proj-123",
      cwd,
      fetchImpl: fakeFetch(),
    });

    const gitignore = await readFile(join(cwd, ".gitignore"), "utf8");
    expect(gitignore.match(/\.flowlens\//g)).toHaveLength(1);
  });

  it("throws when the spec fetch fails", async () => {
    const cwd = await makeTmpDir();
    const fetchImpl = vi.fn(async () => ({ ok: false, status: 404, statusText: "Not Found" }));

    await expect(
      runInit({ url: "https://flowlens.example.com", project: "proj-123", cwd, fetchImpl }),
    ).rejects.toThrow(/404/);
  });

  it("requires url and project", async () => {
    await expect(runInit({ project: "p" })).rejects.toThrow(/--url/);
    await expect(runInit({ url: "https://x" })).rejects.toThrow(/--project/);
  });
});
