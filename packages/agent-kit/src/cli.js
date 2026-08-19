import { parseArgs } from "node:util";

import { runInit } from "./init.js";

const USAGE = `Usage: agent-kit init --url <flowlens-url> [--force]

Installs the FlowLens Claude Code skill and slash commands (.claude/skills/flowlens,
.claude/commands/flowlens) into the current repository. If FLOWLENS_API_TOKEN is set
and the instance at --url is reachable, also resolves the token's project and writes
its OpenAPI spec + config into .flowlens/ (both committed, refreshed on every run).

Options:
  --url <url>        Base URL of the FlowLens instance to connect to
  --force              Overwrite skill/command files that already exist
  -h, --help           Show this message
`;

function statusLabel(status) {
  switch (status) {
    case "created":
      return "created";
    case "overwritten":
      return "overwritten (--force)";
    case "removed":
      return "removed";
    case "skipped":
      return "skipped (already exists; pass --force to overwrite)";
    default:
      return status;
  }
}

export async function runCli(argv) {
  const [command, ...rest] = argv;

  if (!command || command === "-h" || command === "--help") {
    process.stdout.write(USAGE);
    return command ? 0 : 1;
  }

  if (command !== "init") {
    process.stderr.write(`Unknown command: ${command}\n\n${USAGE}`);
    return 1;
  }

  const { values } = parseArgs({
    args: rest,
    options: {
      url: { type: "string" },
      force: { type: "boolean", default: false },
      help: { type: "boolean", short: "h", default: false },
    },
  });

  if (values.help) {
    process.stdout.write(USAGE);
    return 0;
  }

  if (!values.url) {
    process.stderr.write(`--url is required\n\n${USAGE}`);
    return 1;
  }

  const { results, warnings } = await runInit({ url: values.url, force: values.force });
  for (const { path, status } of results) {
    process.stdout.write(`  ${path}: ${statusLabel(status)}\n`);
  }
  for (const warning of warnings) {
    process.stderr.write(`  warning: ${warning}\n`);
  }
  return 0;
}
