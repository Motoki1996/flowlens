# @motokis-lab/agent-kit

Installs FlowLens's Claude Code skill and slash commands into a
project-side repository, so an AI agent working from that repo knows which
FlowLens API to call, in what order, and against what schema.

```bash
export FLOWLENS_API_TOKEN=flt_...   # optional at this point, but needed to also populate .flowlens/
npx @motokis-lab/agent-kit init --url https://flowlens.internal
```

This writes:

| Path | Contents | Committed? |
| --- | --- | --- |
| `.claude/skills/flowlens/SKILL.md` | Shared knowledge (auth, task lifecycle order, branch naming) | yes |
| `.claude/commands/flowlens/refine-backlog.md` | `/flowlens:refine-backlog` | yes |
| `.claude/commands/flowlens/breakdown.md` | `/flowlens:breakdown` | yes |
| `.claude/commands/flowlens/work.md` | `/flowlens:work` | yes |
| `.flowlens/openapi.yaml` | The connected instance's OpenAPI spec | yes |
| `.flowlens/config.json` | `baseUrl` / `projectId` | yes |

The skill and commands are short, stable, and meant to be reviewed and
committed like any other file a colleague or CI agent will read — and need
no live FlowLens instance to install, so they can go into a repo before
one exists. `.flowlens/` mirrors the connected instance and is refreshed
on every `init` run (with or without `--force`); it's committed too, so a
colleague or CI agent who just clones the repo has it without running
`init` themselves.

There's no `--project` flag: the token in `FLOWLENS_API_TOKEN` is already
scoped to exactly one project (see the root README's "API tokens"
section), so `init` resolves it via `GET /api/v1/projects`, which returns
only that one project for a bearer-authenticated request.

If `FLOWLENS_API_TOKEN` isn't set, or the instance at `--url` isn't
reachable, `init` still installs the skill/commands and just warns and
skips `.flowlens/` — re-run `init` later once both are available to
populate it.

`init` never overwrites an existing skill/command file unless `--force` is
passed — re-running it without `--force` only refreshes `.flowlens/`.
