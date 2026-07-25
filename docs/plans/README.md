# Plans

Documents in this directory have a **finite life**. Each one is an agreed
implementation plan for work that has not shipped yet.

This is what separates them from the rest of `docs/`:

| Directory | Content | Life |
| --- | --- | --- |
| `docs/*.md` | Conventions consulted on every change (`architecture`, `ui-design`, `testing`, `storybook`) | Permanent |
| `docs/decisions/` | Why a decision was made | Permanent, append-only |
| `docs/plans/` | How a specific piece of work will be built | Until it ships |

Rules:

- Every plan starts with a **Status** line saying whether it is agreed, in
  progress, or superseded.
- A plan describes the target, so it will contradict the permanent docs while
  the work is unbuilt. Say so explicitly at the point of conflict and link both
  ways, rather than letting a reader discover the contradiction.
- Decisions worth outliving the plan belong in an ADR, **not** here. Write the
  ADR when the decision is taken, not when the plan finishes.
- When the work ships, fold what survives into the permanent docs and **delete
  the plan**. Git history keeps it; a stale plan in `docs/` misleads.

## Current

| Plan | Status |
| --- | --- |
| [issue-sync.md](issue-sync.md) | Agreed, not yet implemented (7 phases) |
