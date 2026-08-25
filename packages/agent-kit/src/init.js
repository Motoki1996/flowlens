import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { writeFileGuarded, removeGitignoreEntry } from "./fs-utils.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const TEMPLATES_DIR = join(__dirname, "..", "templates");

const INSTALL_FILES = [
  { template: "skills/flowlens/SKILL.md", dest: ".claude/skills/flowlens/SKILL.md" },
  { template: "commands/flowlens/refine-backlog.md", dest: ".claude/commands/flowlens/refine-backlog.md" },
  { template: "commands/flowlens/breakdown-epics.md", dest: ".claude/commands/flowlens/breakdown-epics.md" },
  { template: "commands/flowlens/breakdown.md", dest: ".claude/commands/flowlens/breakdown.md" },
  { template: "commands/flowlens/work.md", dest: ".claude/commands/flowlens/work.md" },
];

const DEFAULT_TOKEN_ENV = "FLOWLENS_API_TOKEN";

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
 * Resolves the project a token belongs to. `GET /api/v1/projects` on a
 * bearer-authenticated request always returns exactly that one project
 * (issue #66) — never every project the token's owner has — so no
 * `--project` flag is needed to disambiguate.
 */
export async function resolveProjectId(baseUrl, token, fetchImpl = fetch) {
  const listUrl = `${normalizeBaseUrl(baseUrl)}/api/v1/projects`;
  const res = await fetchImpl(listUrl, { headers: { Authorization: `Bearer ${token}` } });
  if (!res.ok) {
    throw new Error(`failed to resolve project from ${listUrl}: ${res.status} ${res.statusText}`);
  }
  const projects = await res.json();
  if (!Array.isArray(projects) || projects.length !== 1) {
    throw new Error(`expected the token to resolve to exactly one project, got ${projects?.length ?? 0}`);
  }
  return projects[0].id;
}

/**
 * Installs the FlowLens agent-kit into `cwd`.
 *
 * The skill + slash commands are always installed (skipped when already
 * present, unless `force`) — they need no live instance, so a repo can
 * carry them before FlowLens is even reachable. The spec + config under
 * `.flowlens/` mirror the connected instance and are refreshed whenever
 * that instance is actually reachable; if it isn't (or no token is set to
 * resolve the project with), that part is skipped with a warning rather
 * than failing the whole install.
 */
export async function runInit({
  url,
  force = false,
  cwd = process.cwd(),
  fetchImpl = fetch,
  env = process.env,
  tokenEnvVar = DEFAULT_TOKEN_ENV,
}) {
  if (!url) throw new Error("--url is required");

  const baseUrl = normalizeBaseUrl(url);
  const results = [];
  const warnings = [];

  for (const file of INSTALL_FILES) {
    const content = await readFile(join(TEMPLATES_DIR, file.template), "utf8");
    const status = await writeFileGuarded(join(cwd, file.dest), content, { force });
    results.push({ path: file.dest, status });
  }

  const gitignoreStatus = await removeGitignoreEntry(join(cwd, ".gitignore"), ".flowlens/");
  if (gitignoreStatus !== "skipped") {
    results.push({ path: ".gitignore", status: gitignoreStatus });
  }

  const token = env[tokenEnvVar];
  let projectId = null;
  if (!token) {
    warnings.push(`${tokenEnvVar} is not set — skipping .flowlens/ (no token to resolve the project with)`);
  } else {
    try {
      projectId = await resolveProjectId(baseUrl, token, fetchImpl);
    } catch (err) {
      warnings.push(`could not reach ${baseUrl} to resolve the project — skipping .flowlens/ (${err.message})`);
    }
  }

  if (projectId) {
    try {
      const spec = await fetchOpenapiSpec(baseUrl, fetchImpl);
      const specStatus = await writeFileGuarded(join(cwd, ".flowlens", "openapi.yaml"), spec, {
        alwaysOverwrite: true,
      });
      results.push({ path: ".flowlens/openapi.yaml", status: specStatus });

      const config = { baseUrl, projectId, updatedAt: new Date().toISOString() };
      const configStatus = await writeFileGuarded(
        join(cwd, ".flowlens", "config.json"),
        `${JSON.stringify(config, null, 2)}\n`,
        { alwaysOverwrite: true },
      );
      results.push({ path: ".flowlens/config.json", status: configStatus });
    } catch (err) {
      warnings.push(`could not fetch the OpenAPI spec from ${baseUrl} — skipping .flowlens/ (${err.message})`);
    }
  }

  return { results, warnings };
}
