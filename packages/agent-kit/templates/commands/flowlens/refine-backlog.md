---
description: FlowLens backlog の要件を、リポジトリのコードを踏まえて具体化し、番号付き要件として書き戻す
argument-hint: <backlogId>
allowed-tools: Bash(cat .flowlens/config.json), Bash(curl:*), Read, Grep, Glob
---

Read the `flowlens` skill first for auth and shared conventions.

## Target backlog

Backlog ID: $1

## Steps

1. Load `.flowlens/config.json` for `baseUrl`/`projectId`, and read the
   token from its environment variable.
2. `GET {baseUrl}/api/v1/backlogs/$1` (and, if useful for surrounding
   context, `GET {baseUrl}/api/v1/projects/{projectId}/backlogs`).
3. Read the parts of this repository the backlog's description touches —
   don't refine requirements against an assumption of what the code does.
4. Rewrite the backlog's `description` as a set of concrete, numbered
   requirements: `R1`, `R2`, `R3`, … This numbering is the **only**
   traceability mechanism between requirements and the tasks
   `/flowlens:breakdown` will create from them — a backlog has no
   structured acceptance-criteria field, only this free-text description.
   Keep each requirement testable and scoped to one concern.
5. Decide the backlog's `baseBranch` (the branch its tasks should branch
   from) if it isn't already set.
6. `PATCH {baseUrl}/api/v1/backlogs/$1` with the updated `description` and
   `baseBranch` in one request.

## Output

Report the requirement list (`R1`…`Rn`) and the `baseBranch` you set, so
the user can review before breaking the backlog down.

Then say which of the two next steps you'd take and why: a backlog large
enough to want a coarse middle rung goes through `/flowlens:breakdown-epics
<backlogId>` first, and one small enough to cut straight into tasks goes to
`/flowlens:breakdown <backlogId>`. The epic rung is optional — recommend it
when the requirements clearly divide along boundaries the codebase has
(separate screens, separate endpoint groups), or when parts of the backlog
target different base branches.
