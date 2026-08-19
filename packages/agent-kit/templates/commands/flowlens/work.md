---
description: 1つの FlowLens タスクについて、設計→実装のライフサイクルを実行する（SDD、逐次実行前提）
argument-hint: <taskId>
allowed-tools: Bash(cat .flowlens/config.json), Bash(curl:*), Bash(git checkout:*), Bash(git branch:*), Read, Edit, Write, Grep, Glob
---

Read the `flowlens` skill first — this command is the lifecycle it
describes, run end to end for one task. **Do not run this concurrently
with another `/flowlens:work` invocation on the same repo** — there is no
task-claiming mechanism yet, so two agents can race onto the same task.

## Target task

Task ID: $1

## Steps

1. Load `.flowlens/config.json` and the token.
2. `GET {baseUrl}/api/v1/projects/{projectId}/task-dependencies`, filter to
   edges where this task is the successor, and confirm every predecessor
   task is closed. If not, stop and report which predecessor is blocking.
3. `GET {baseUrl}/api/v1/tasks/$1/context` for acceptance criteria, scope
   (`allowedScope`/`forbiddenScope`), the GitLab `issueIid`, and the
   backlog's `baseBranch` (resolved into this response).
4. **Design phase:**
   - `POST {baseUrl}/api/v1/tasks/$1/design-started`
   - Design the change within `allowedScope`, respecting `forbiddenScope`.
   - Post the design as `POST {baseUrl}/api/v1/tasks/$1/comments`.
   - Record/refine acceptance criteria via `PUT
     {baseUrl}/api/v1/tasks/$1/ai-context`.
   - **Stop here and wait for human confirmation before implementing.**
     The point of design-first is that it's reviewable before code exists;
     skip this pause only if the user has explicitly said to run
     unattended.
5. **Implementation phase:**
   - `POST {baseUrl}/api/v1/tasks/$1/implementation-started`
   - `PATCH {baseUrl}/api/v1/tasks/$1` with `{"progress": "in_progress"}`.
   - Branch from the backlog's `baseBranch` (or the repo default if unset),
     named so it embeds the GitLab issue iid per the skill's branch-naming
     rule — e.g. `issue-<issueIid>-<slug>`.
   - Implement, respecting `allowedScope`/`forbiddenScope`.
   - Open an MR with `Closes #<issueIid>` in its description.
   - If blocked on something outside this task (review, flaky CI,
     external dependency), `PATCH` `{"progress": "on_hold"}` and post a
     comment explaining why.
6. **Close:**
   - `PATCH {baseUrl}/api/v1/tasks/$1` with `{"progress": "done"}`.
   - `POST {baseUrl}/api/v1/tasks/$1/close`.

## Output

Report which phase you reached, the branch/MR you created (if any), and
anything you're blocked on.
