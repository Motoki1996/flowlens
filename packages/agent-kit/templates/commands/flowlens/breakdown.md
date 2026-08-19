---
description: 具体化済みの FlowLens backlog を、コードの影響範囲を踏まえてタスクに分解し、依存関係とともに一括登録する
argument-hint: <backlogId>
allowed-tools: Bash(cat .flowlens/config.json), Bash(curl:*), Read, Grep, Glob
---

Read the `flowlens` skill first for auth and shared conventions. This
command expects the backlog to already have numbered requirements
(`R1`, `R2`, …) — run `/flowlens:refine-backlog` first if it doesn't.

## Target backlog

Backlog ID: $1

## Steps

1. Load `.flowlens/config.json` and the token, and `GET
   {baseUrl}/api/v1/backlogs/$1` for the refined requirements.
2. Read the relevant parts of this repository to ground the breakdown in
   actual code structure, not just the requirement text.
3. Split the work into tasks. For each task, decide:
   - `title`, `description`
   - `size` (`xs`/`s`/`m`/`l`/`xl`)
   - `priority`
   - which requirement(s) it satisfies (record the `R#` reference(s) in the
     task's `aiContext`, e.g. "Implements R2.")
   - `allowedScope` / `forbiddenScope` — paths the task may and may not
     touch
   - `acceptanceCriteria`
4. Decide dependencies between the new tasks (predecessor → successor).
   These determine the order `/flowlens:work` can run them in — a task
   whose predecessor isn't closed yet should not be started.
5. Submit everything in one call: `POST
   {baseUrl}/api/v1/projects/{projectId}/tasks/bulk` with `tasks` (each
   carrying a request-scoped `ref`, `backlogId: "$1"`, and an inline
   `aiContext`) and `dependencies` (`{predecessorRef, successorRef}`
   pairs referencing those same `ref`s). This is all-or-nothing — a
   validation failure on any task or dependency leaves nothing written, so
   fix and resubmit rather than retrying piecemeal.

## Output

List the created tasks (title, size, priority, requirement reference) and
the dependency edges between them.
