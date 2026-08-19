# @flowlens/agent-kit

Installs FlowLens's Claude Code skill and slash commands into a
project-side repository, so an AI agent working from that repo knows which
FlowLens API to call, in what order, and against what schema.

```bash
npx @flowlens/agent-kit init --url https://flowlens.internal --project <projectId>
```

This writes:

| Path | Contents | Committed? |
| --- | --- | --- |
| `.claude/skills/flowlens/SKILL.md` | Shared knowledge (auth, task lifecycle order, branch naming) | yes |
| `.claude/commands/flowlens/refine-backlog.md` | `/flowlens:refine-backlog` | yes |
| `.claude/commands/flowlens/breakdown.md` | `/flowlens:breakdown` | yes |
| `.claude/commands/flowlens/work.md` | `/flowlens:work` | yes |
| `.flowlens/openapi.yaml` | The connected instance's OpenAPI spec | no (gitignored) |
| `.flowlens/config.json` | `baseUrl` / `projectId` | no (gitignored) |

The skill and commands are short, stable, and meant to be reviewed and
committed like any other file a colleague or CI agent will read. The spec
and config are generated from the connected instance and change on every
release, so they're fetched fresh on every `init` run (with or without
`--force`) and are gitignored rather than committed.

`init` never overwrites an existing skill/command file unless `--force` is
passed — re-running it without `--force` only refreshes `.flowlens/`.

An agent working a task still needs a project API token (see the root
README's "API tokens" section) reachable through an environment variable —
`agent-kit init` does not create or store one.
