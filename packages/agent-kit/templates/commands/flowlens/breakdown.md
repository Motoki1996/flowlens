---
description: 具体化済みの FlowLens backlog または epic を、コードの影響範囲を踏まえてタスクに分解し、依存関係とともに一括登録する
argument-hint: <backlogId|epicId>
allowed-tools: Bash(cat .flowlens/config.json), Bash(curl:*), Read, Grep, Glob
---

Read the `flowlens` skill first for auth and shared conventions. This
command expects numbered requirements (`R1`, `R2`, …) to exist already —
on the backlog itself, or on the backlog behind the epic you named. Run
`/flowlens:refine-backlog` on that backlog first if they don't.

## Target

ID: $1 — either a **backlog** or an **epic**, whichever the caller named.
The tasks are filed under that object.

## Steps

1. Load `.flowlens/config.json` and the token, then resolve what `$1` is:
   `GET {baseUrl}/api/v1/epics/$1` first, and on 404 `GET
   {baseUrl}/api/v1/backlogs/$1`. Read the refined requirements from
   whichever one answered. For an epic, also `GET
   {baseUrl}/api/v1/backlogs/{its backlogId}` — the epic's own
   requirements are a slice of the backlog's, and its empty fields fall
   through to the backlog's.
2. Read the relevant parts of this repository to ground the breakdown in
   actual code structure, not just the requirement text.
3. Split the work into tasks. For each task, decide:
   - `title`, `description`
   - `size` (`xs`/`s`/`m`/`l`/`xl`)
   - `priority`
   - which requirement(s) it satisfies (record the `R#` reference(s) in the
     task's `aiContext`, e.g. "Implements R2.")
   - `acceptanceCriteria`

   `baseBranch` and `allowedScope`/`forbiddenScope` are not per-task
   fields — they live on the epic and the backlog (both fetched in step 1)
   and apply to every task filed there. They resolve **epic first, then
   backlog, per field**: an epic that sets only `baseBranch` still
   inherits the backlog's scope. If the scope needs setting or refining,
   `PATCH` the object you were given — the *epic* when `$1` is an epic,
   not the backlog above it — rather than inventing a per-task scope.
4. Decide dependencies between the new tasks (predecessor → successor).
   These determine the order `/flowlens:work` can run them in — a task
   whose predecessor isn't closed yet should not be started.
5. Submit everything in one call: `POST
   {baseUrl}/api/v1/projects/{projectId}/tasks/bulk` with `tasks` (each
   carrying a request-scoped `ref`, either `epicId: "$1"` or
   `backlogId: "$1"` — whichever `$1` turned out to be, never both, since
   an `epicId` already files the task in that epic's backlog — and an
   inline `aiContext`) and `dependencies` (`{predecessorRef, successorRef}`
   pairs referencing those same `ref`s). This is all-or-nothing — a
   validation failure on any task or dependency leaves nothing written, so
   fix and resubmit rather than retrying piecemeal.

## Output

List the created tasks (title, size, priority, requirement reference) and
the dependency edges between them.
