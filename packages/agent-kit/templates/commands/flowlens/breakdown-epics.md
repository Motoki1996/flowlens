---
description: 具体化済みの FlowLens backlog を、画面単位などの粗い粒度の epic に分解して登録する
argument-hint: <backlogId>
allowed-tools: Bash(cat .flowlens/config.json), Bash(curl:*), Read, Grep, Glob
---

Read the `flowlens` skill first for auth and shared conventions. This
command expects the backlog to already have numbered requirements
(`R1`, `R2`, …) — run `/flowlens:refine-backlog` first if it doesn't.

This is the **optional** middle rung. A backlog small enough to break
straight into tasks doesn't need it: run `/flowlens:breakdown <backlogId>`
instead and skip this entirely.

## Target backlog

Backlog ID: $1

## Steps

1. Load `.flowlens/config.json` and the token, and `GET
   {baseUrl}/api/v1/backlogs/$1` for the refined requirements, base branch
   and allowed/forbidden scope.
2. Read the relevant parts of this repository to ground the split in
   actual code structure, not just the requirement text.
3. Split the backlog into **epics** — coarse units of a few tasks each,
   cut along a boundary the codebase actually has: one screen, one
   endpoint group, one migration + API pair. An epic that maps to a single
   task is too small; one that spans the whole backlog is too large.
4. For each epic, decide:
   - `name`, `description` (record which `R#` it covers, e.g. "Covers R2,
     R3.")
   - `priority`, and `startDate`/`dueOn` if the backlog's own period
     divides sensibly
   - `baseBranch` — **only when it differs from the backlog's**. This is
     the field the epic rung mainly exists for: two epics in one backlog
     routinely target different release branches. Left empty, the
     backlog's own base branch applies.
   - `allowedScope`/`forbiddenScope` — again only when narrower than the
     backlog's. Resolution runs per *field*, so an epic may override the
     branch and still inherit the scope.
5. Create each one with `POST
   {baseUrl}/api/v1/projects/{projectId}/epics`, passing `backlogId: "$1"`.
   There is deliberately no bulk endpoint here: epics have no dependency
   graph between them, so a partial failure is re-runnable rather than a
   corrupt result — just create the ones that are missing and carry on.
6. Do **not** create tasks. Each epic is broken down separately, by
   `/flowlens:breakdown <epicId>`, so the task-level detail is decided
   with that one epic's scope in view rather than the whole backlog's.

## Output

List the created epics (name, requirement references, base branch if it
differs from the backlog's) and the `/flowlens:breakdown <epicId>` command
to run for each.
