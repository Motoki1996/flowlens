import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { writeFileGuarded, ensureGitignoreEntry } from "./fs-utils.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const TEMPLATES_DIR = join(__dirname, "..", "templates");

const INSTALL_FILES = [
  { template: "skills/flowlens/SKILL.md", dest: ".claude/skills/flowlens/SKILL.md" },
  { template: "commands/flowlens/refine-backlog.md", dest: ".claude/commands/flowlens/refine-backlog.md" },
  { template: "commands/flowlens/breakdown.md", dest: ".claude/commands/flowlens/breakdown.md" },
  { template: "commands/flowlens/work.md", dest: ".claude/commands/flowlens/work.md" },
];

function normalizeBaseUrl(url) {
  return url.replace(/\/+$/, "");
}

export async function fetchOpenapiSpec(baseUrl, fetchImpl = fetch) {
  const specUrl = `${normalizeBaseUrl(baseUrl)}/openapi.yaml`;
  const res = await fetchImpl(specUrl);
  if (!res.ok) {
    throw new Error(`failed to fetch ${specUrl}: ${res.status} ${res.statusText}`);
  }
  return res.text();
}

/**
 * Installs the FlowLens agent-kit into `cwd`: the skill + slash commands
 * (skipped when already present, unless `force`), and the spec + config
 * under `.flowlens/` (always refreshed, since they mirror the connected
 * instance rather than being hand-edited).
 */
export async function runInit({ url, project, force = false, cwd = process.cwd(), fetchImpl = fetch }) {
  if (!url) throw new Error("--url is required");
  if (!project) throw new Error("--project is required");

  const baseUrl = normalizeBaseUrl(url);
  const results = [];

  const spec = await fetchOpenapiSpec(baseUrl, fetchImpl);
  const specStatus = await writeFileGuarded(join(cwd, ".flowlens", "openapi.yaml"), spec, {
    alwaysOverwrite: true,
  });
  results.push({ path: ".flowlens/openapi.yaml", status: specStatus });

  const config = {
    baseUrl,
    projectId: project,
    updatedAt: new Date().toISOString(),
  };
  const configStatus = await writeFileGuarded(
    join(cwd, ".flowlens", "config.json"),
    `${JSON.stringify(config, null, 2)}\n`,
    { alwaysOverwrite: true },
  );
  results.push({ path: ".flowlens/config.json", status: configStatus });

  for (const file of INSTALL_FILES) {
    const content = await readFile(join(TEMPLATES_DIR, file.template), "utf8");
    const status = await writeFileGuarded(join(cwd, file.dest), content, { force });
    results.push({ path: file.dest, status });
  }

  const gitignoreStatus = await ensureGitignoreEntry(join(cwd, ".gitignore"), ".flowlens/");
  results.push({ path: ".gitignore", status: gitignoreStatus });

  return results;
}
