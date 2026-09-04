# API tokens and AI agents

## API tokens

An AI agent (or any external integration) reads and writes a project's
tasks through a project-scoped bearer token rather than a user session. A
token **acts as the project's owner** — every request it makes is
authorized exactly the way that owner's session would be — but it is
confined to the one project it was issued for and to a fixed allowlist of
routes; see [ADR-0009](../decisions/0009-why-project-scoped-api-tokens.md)
for why it's built that way.

### Issuing a token

From the project's single view (`/projects/{projectId}`), open the **API
tokens** card, click **Issue token**, and fill in a name, a scope
(**Read-only** or **Read & write**), and an optional expiry date. The raw
token is shown exactly once, in the "Token issued" dialog right after
creation — FlowLens stores only its SHA-256 hash, the same as a session
cookie, so if you lose it you have to issue a new one. Revoke a token any
time with **Revoke** on its row in the same card.

The same action, direct against the API (session auth; this is the only
token-management route a token itself can never call — see
[What a token can't reach](#what-a-token-cant-reach) below):

```bash
curl -X POST "$API_BASE_URL/api/v1/projects/$PROJECT_ID/api-tokens" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -d '{"name": "ci-agent", "scopes": ["read", "write"]}'
```

```jsonc
{
  "id": "b7e1...",
  "projectId": "a1b2...",
  "name": "ci-agent",
  "scopes": ["read", "write"],
  "tokenPrefix": "flt_9f3a2c1d",
  "createdAt": "2026-08-02T00:00:00Z",
  "token": "flt_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d" // only ever present here
}
```

### Calling the API

Every call authenticates with `Authorization: Bearer <token>` instead of
the session cookie:

```bash
curl "$API_BASE_URL/api/v1/tasks/$TASK_ID/context" \
  -H "Authorization: Bearer flt_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d"
```

```jsonc
// GET /api/v1/tasks/{taskID}/context
{
  "id": "3fa2...",
  "projectId": "a1b2...",
  "backlogId": "c3d4...",       // null when unfiled (Unclassified)
  "title": "Fix login redirect",
  "description": "…",
  "status": "open",             // "open" | "closed"
  "progress": "in_progress",    // "not_started" | "in_progress" | "on_hold" | "done"
  "progressGuidance": "Update this task's progress as you work, via PATCH …",
  "assigneeGitlabUserId": 42,   // null when unassigned
  "assigneeGitlabUsername": "octocat",
  "labels": ["bug"],
  "dueOn": "2026-08-01T00:00:00Z", // null when unset
  "updatedAt": "2026-07-30T03:00:00Z",
  "gitlab": {                   // null when never linked to a GitLab project
    "syncStatus": "synced",     // "synced" | "pending" | "failed"
    "lastError": "",
    "lastSyncedAt": "2026-07-30T03:00:00Z",
    "issueIid": 7,
    "webUrl": "https://gitlab.example.com/group/demo/-/issues/7",
    "projectPath": "group/demo"
  },
  "acceptanceCriteria": "Given/When/Then …",
  "aiContext": "Legacy payments module …",
  "allowedScope": "internal/payments/**",
  "forbiddenScope": "internal/auth/**"
}
```

A token is scoped to the project it was issued for — a request against a
task from a different project gets the same 404 a foreign session gets, not
a 403, so a token can't distinguish "not yours" from "does not exist". All
four fields are `""`, never `null`, until set: `acceptanceCriteria`/
`aiContext` via `PUT /api/v1/tasks/{taskID}/ai-context`, `allowedScope`/
`forbiddenScope` (and `baseBranch`) via the task's epic and then its backlog,
resolved per field (see [An epic's base branch and change scope](epics.md#an-epics-base-branch-and-change-scope)) —
`""` either way when neither sets one.

To list several tasks at once (e.g. an agent polling its queue), use
`GET /api/v1/projects/{projectID}/tasks/context?status=open&per_page=20`,
which returns the same per-task shape plus `nextPage` (`0` when there is no
next page). `?updated_since=<RFC 3339 timestamp>` filters to tasks touched
at or after it, for incremental polling.

### Progress convention for agents (issue #170)

FlowLens can only measure how long work actually takes if an agent keeps
`progress` current while it works — `PATCH /api/v1/tasks/{taskID}` with
`{"progress": "..."}`, the same `write`-scoped route as any other task edit:

- `in_progress` — set it the moment the agent starts working the task.
- `on_hold` — set it whenever the agent is blocked or waiting on something
  outside the task (a human review, a flaky CI run, a dependency). This is
  the value the eventual bottleneck detection leans on hardest: leave it
  unused and wait time silently gets counted as work time.
- `done` — set it once the work is finished.

Never PATCH `status`; that field mirrors the GitLab issue's open/closed
state and is kept in sync automatically in both directions, independently
of `progress` (see [Task & backlog progress](tasks.md#task--backlog-progress)).

Every `PATCH` that changes `progress` is recorded as a
`task_progress_events` row (issue #169), attributed to `"agent"` when the
caller is a bearer token — that log is what
[Flow metrics](metrics.md#flow-metrics-issue-171) reads lead time and wait time off
of, so an agent that never calls `PATCH` leaves nothing to measure. This
convention is not only written down
here: the `progressGuidance` field above carries the same instructions in
every `GET .../context` response, since that response is the one thing an
agent working through a token reliably reads, unlike this README.

### Activity log (comments)

`GET /api/v1/tasks/{taskID}/context` above also carries `comments`: the
task's most recent activity-log entries (at most 20, oldest first) — the
return path for an agent that has been reading a task's context but had no
way to report back what it did or where it got stuck. Post to that log with
`POST /api/v1/tasks/{taskID}/comments`:

```bash
curl -X POST "$API_BASE_URL/api/v1/tasks/$TASK_ID/comments" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer flt_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d" \
  -d '{"body": "Pushed a fix in MR !12; CI is green."}'
```

```jsonc
// POST /api/v1/tasks/{taskID}/comments
{
  "id": "e4a1...",
  "taskId": "3fa2...",
  "authorUserId": null,       // set for a human's comment, null for a token's
  "authorTokenId": "b7e1...", // set for a token's comment, null for a human's
  "authorKind": "agent",      // "user" | "agent" | "gitlab"
  "body": "Pushed a fix in MR !12; CI is green.",
  "createdAt": "2026-08-10T00:00:00Z",
  "updatedAt": "2026-08-10T00:00:00Z"
}
```

`GET /api/v1/tasks/{taskID}/comments` returns the full log, oldest first,
with no page cap. `DELETE /api/v1/task-comments/{commentID}` removes one
comment — a caller (session or token) can only delete its own; a
`"gitlab"`-authored comment has no user or token to match, so it cannot be
deleted through this endpoint at all.

A `"user"` or `"agent"` comment on a task linked to GitLab is also pushed to
the linked issue as a note, and a `"gitlab"`-authored comment is one GitLab
itself posted, mirrored in by the inbound webhook — see [Task comments sync
with GitLab issue discussions](gitlab-sync.md#task-comments-sync-with-gitlab-issue-discussions).

### Scopes

| Scope | Grants |
| --- | --- |
| `read` | Every allowlisted `GET`, including both context endpoints above. Every token has at least this scope. |
| `write` | Everything `read` grants, plus create/update/delete on tasks, backlogs and task-dependencies. |

`scopes: ["write"]` alone is expanded to `["read","write"]` at creation —
write always implies read, so a route never has to check for `write` without
also accepting a plain `read` token where read access is all it needs.
Omitting `scopes` on creation defaults to `["read"]`.

### Reachable endpoints

Beyond the two context endpoints above, a token can reach this fixed
allowlist of the regular task-tracker routes:

| Method | Path | Scope |
| --- | --- | --- |
| GET | `/projects` (only the token's own project, never every project its owner has) | `read` |
| GET | `/projects/{projectID}` | `read` |
| GET | `/projects/{projectID}/backlogs` | `read` |
| POST | `/projects/{projectID}/backlogs` | `write` |
| GET | `/backlogs/{backlogID}` | `read` |
| PATCH, DELETE | `/backlogs/{backlogID}` | `write` |
| GET | `/projects/{projectID}/tasks` | `read` |
| POST | `/projects/{projectID}/tasks` | `write` |
| POST | `/projects/{projectID}/tasks/bulk` | `write` |
| GET | `/tasks/{taskID}` | `read` |
| PATCH, DELETE | `/tasks/{taskID}` | `write` |
| POST | `/tasks/{taskID}/close`, `/reopen`, `/assign-backlog`, `/sync-retry` | `write` |
| PUT | `/tasks/{taskID}/ai-context` | `write` |
| GET | `/tasks/{taskID}/comments` | `read` |
| POST | `/tasks/{taskID}/comments` | `write` |
| DELETE | `/task-comments/{commentID}` (own comment only) | `write` |
| GET | `/projects/{projectID}/task-dependencies` | `read` |
| POST | `/projects/{projectID}/task-dependencies` | `write` |
| DELETE | `/task-dependencies/{dependencyID}` | `write` |
| GET | `/linked-gitlab-projects/{linkID}/sync-runs` | `read` |

A single-resource URL (`{taskID}`, `{backlogID}`, `{dependencyID}`,
`{commentID}`, `{linkID}`) is checked against the token's own project the
same way `{projectID}` is: a resource in a *different* project owned by the
same user gets the same 404 as one that doesn't exist.

### What a token can't reach

Everything else stays session-only, permanently, regardless of scope —
there is no scope that unlocks these, because each one lets an existing
credential either read a second secret or reshape the project's own trust
boundary:

- **The project's GitLab connection** (`PUT`/`GET`/`DELETE
  .../gitlab-connection`, `.../gitlab-connection/test`,
  `.../gitlab-connection/available-projects`) — it holds an encrypted GitLab
  access token; a project API token must never be a path to a second,
  more powerful secret.
- **API tokens themselves** (`GET`/`POST .../api-tokens`,
  `DELETE /api-tokens/{tokenID}`) — otherwise a read-only token could mint
  itself a write token, defeating the scope check entirely.
- **Project membership** (`GET`/`POST .../members`,
  `PATCH`/`DELETE .../members/{userID}`) — who can access a project at all
  is a project-management decision, the same reasoning as API tokens above.
- **Creating or deleting a project** (`POST /projects`,
  `DELETE /projects/{projectID}`) — a token could otherwise create or
  destroy its own footing.
- **Linked-GitLab-project management and webhook registration**
  (`POST`/`PATCH`/`DELETE .../linked-gitlab-projects*`,
  `.../webhook`, `.../webhook-events*`) — these change what the project
  syncs with and how, which is a connection-level decision, not a
  task-tracker read/write.

`Authorization` is also never added to the web app's CORS-allowed request
headers, so a bearer token stays usable only for direct, server-to-server
calls, never from browser script.

## Agent Kit: setting up an AI agent in the repo it works in (issue #203)

An OpenAPI spec (served at `GET /openapi.yaml`, see above) resolves *what
schema* an endpoint takes, but not *what order* to call things in — that's
what `@motokis-lab/agent-kit` installs into the repository an AI agent
actually works in, as a Claude Code skill and four slash commands.

```bash
export FLOWLENS_API_TOKEN=flt_...   # optional, see below
npx @motokis-lab/agent-kit init --url https://flowlens.internal
```

This is deliberately not an npm dependency of the consumer repo: FlowLens
tracks repos in any language, and a self-hosted instance's version drifts
from whatever a published package would pin, which would eventually point
an agent at endpoints that don't exist on that instance. `npx` needs no
`package.json` in the target repo, and `init` re-fetches the spec from the
connected instance itself rather than bundling one.

There is no `--project` flag: a project API token (see "API tokens" above)
is already scoped to exactly one project, so `init` resolves it itself via
`GET /api/v1/projects`, which returns only the token's own project for a
bearer-authenticated request — never every project its owner has.

`init` writes:

| Path | Contents | Committed? |
| --- | --- | --- |
| `.claude/skills/flowlens/SKILL.md` | Auth, the task lifecycle's call order, branch-naming rule | yes |
| `.claude/commands/flowlens/refine-backlog.md` | `/flowlens:refine-backlog` — turn a backlog into numbered requirements | yes |
| `.claude/commands/flowlens/breakdown-epics.md` | `/flowlens:breakdown-epics` — split a refined backlog into estimated [epics](epics.md#epics-an-optional-layer-between-a-backlog-and-its-tasks); optional | yes |
| `.claude/commands/flowlens/breakdown.md` | `/flowlens:breakdown` — split a backlog **or one epic** into sized, scoped, dependency-ordered tasks via [bulk task creation](tasks.md#bulk-task-creation) | yes |
| `.claude/commands/flowlens/work.md` | `/flowlens:work` — run one task's design → implementation lifecycle | yes |
| `.flowlens/openapi.yaml` | The connected instance's OpenAPI spec | yes |
| `.flowlens/config.json` | `baseUrl` / `projectId` | yes |

The skill and commands need no live FlowLens instance and are always
installed (skipped if already present, unless `--force`) — they need to
reach a cloned-but-not-run colleague and a CI-triggered agent, not just
whoever ran `init`. `.flowlens/` mirrors the connected instance instead,
so it's only written when `FLOWLENS_API_TOKEN` is set **and** `--url` is
reachable; `init` resolves the project through that token, fetches the
spec, and writes both files, refreshing them on every subsequent run
(`--force` or not) since they change on every FlowLens release. When
either the token or the instance is missing, `init` still installs the
skill/commands, only warns, and leaves `.flowlens/` for a later run.
Both are committed, unlike the previous gitignored versions, so a
colleague or CI agent who just clones the repo has them without running
`init` themselves.

Commands live under the `.claude/commands/flowlens/` directory namespace
(`/flowlens:breakdown`, not a flat `/breakdown`), so they can't collide
with a repo's own commands and uninstalling is just deleting the
directory. The skill assumes the same project API token is reachable
through `FLOWLENS_API_TOKEN` at call time — `init` never stores it — and
that only one `/flowlens:work` runs at a time: there is no task-claiming
endpoint yet, so concurrent agents can race onto the same task.

