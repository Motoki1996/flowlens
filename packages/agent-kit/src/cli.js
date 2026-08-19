import { parseArgs } from "node:util";

import { runInit } from "./init.js";

const USAGE = `Usage: agent-kit init --url <flowlens-url> --project <projectId> [--force]

Installs the FlowLens Claude Code skill and slash commands (.claude/skills/flowlens,
.claude/commands/flowlens) into the current repository, and fetches the connected
instance's OpenAPI spec into .flowlens/ (gitignored).

Options:
  --url <url>        Base URL of the FlowLens instance to connect to
  --project <id>      Project ID tasks/backlogs will be read from and written to
  --force              Overwrite skill/command files that already exist
  -h, --help           Show this message
`;

function statusLabel(status) {
  switch (status) {
    case "created":
      return "created";
    case "overwritten":
      return "overwritten (--force)";
    case "appended":
      return "updated";
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
      project: { type: "string" },
      force: { type: "boolean", default: false },
      help: { type: "boolean", short: "h", default: false },
    },
  });

  if (values.help) {
    process.stdout.write(USAGE);
    return 0;
  }

  if (!values.url || !values.project) {
    process.stderr.write(`--url and --project are required\n\n${USAGE}`);
    return 1;
  }

  const results = await runInit({ url: values.url, project: values.project, force: values.force });
  for (const { path, status } of results) {
    process.stdout.write(`  ${path}: ${statusLabel(status)}\n`);
  }
  return 0;
}
