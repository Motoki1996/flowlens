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
2. `GET {baseUrl}/api/v1/projects/{projectId}/epics?backlog_id=$1` for the
   epics this backlog already has. Do not skip this: step 6 has no bulk
   endpoint and no idempotency key, so on a re-run this list is the only
   thing standing between you and a duplicate set of epics. Create only
   what is missing from it, and say in the output which ones you skipped.
3. Read the relevant parts of this repository to ground the split in
   actual code structure, not just the requirement text.
4. Split the backlog into **epics** — coarse units of a few tasks each,
   cut along a boundary the codebase actually has: one screen, one
   endpoint group, one migration + API pair. An epic that maps to a single
   task is too small; one that spans the whole backlog is too large.
5. For each epic, decide:
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
   - `estimatedPoints` — a provisional estimate of the epic's size, in
     points. **Derive it from this project's own data, never from a bare
     hunch**: an absolute number pulled out of the air cannot be
     calibrated against anything later. Concretely — `GET
     {baseUrl}/api/v1/projects/{projectId}/tasks?per_page=200` for the
     existing tasks (the list is paged — read them from the response's
     `tasks` array, and `totalCount` tells you how many there are in all),
     look at how this project's work actually divides into `size` values,
     then estimate how many tasks of which sizes this epic will become and
     total them on the scale velocity uses: `xs`=1, `s`=2, `m`=3, `l`=5,
     `xl`=8. An epic you expect to be two `m` tasks and one `l` is 11.
     Say in the epic's `description` which counts you assumed, so the
     estimate can be argued with. Omit the field entirely if you genuinely
     cannot ground it — the API reports unestimated epics as such, which is
     more useful than a fabricated number, and 0 is rejected precisely so
     it cannot be used as a shrug.
6. Create each missing one with `POST
   {baseUrl}/api/v1/projects/{projectId}/epics`, passing `backlogId: "$1"`.
   Requires a token with `write` scope; a read-only token gets 403 here.
   There is deliberately no bulk endpoint: epics have no dependency graph
   between them, so a partial failure is re-runnable rather than a corrupt
   result — re-run this command and step 2 will tell you what is left.
7. Do **not** create tasks. Each epic is broken down separately, by
   `/flowlens:breakdown <epicId>`, so the task-level detail is decided
   with that one epic's scope in view rather than the whole backlog's.
   `estimatedPoints` is what stands in for those tasks until then: it is
   why the project's velocity forecast still sees this work today, and it
   is superseded automatically — not overwritten — once the tasks exist.

## Output

List the created epics (name, requirement references, `estimatedPoints`
and what it assumes, base branch if it differs from the backlog's) and the
`/flowlens:breakdown <epicId>` command to run for each. Name separately any
epic that already existed and was left alone, so a re-run reads as a re-run
rather than as having done nothing.
