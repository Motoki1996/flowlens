# 0010. Why project membership is a new `project_members` table, not `organizations`

- **Status:** Accepted
- **Date:** 2026-08-09

## Context

`projects.owner_user_id` is a single owner: nothing in the schema lets a
second person touch a project. That is fine for the login/signup MVP, but
FlowLens exists to show a *team's* delivery process, and a tool for a team
that only one account can use is a contradiction.

Two tables already exist for this kind of thing: `organizations` and
`organization_members`, created in `000001_init` but never wired to
anything. They anticipated a GitLab-group-shaped hierarchy (organization →
projects), not project-level sharing.

This is the first of a two-issue series. This issue adds the schema only —
no route, handler, or ownership check changes. The ownership-check
replacement (project-scoped access moving from `owner_user_id` equality to
membership) is issue #99. Splitting it this way keeps each PR reviewable and
means a schema mistake is caught before any behaviour depends on it.

## Decision

**Add a new `project_members` table; leave `organizations` /
`organization_members` alone.**

- `organizations` models a GitLab-group-shaped hierarchy that groups
  *projects*, one level above where sharing needs to happen. Reusing it for
  project membership would conflate "which GitLab group does this mirror"
  with "who can access this project" — two different questions that happen
  to share a shape. Keeping them separate leaves `organizations` free for
  its original purpose (a future GitLab group mirror) without a membership
  meaning baked in by accident.
- `project_members` carries three roles: `owner`, `member`, `viewer`. This
  is deliberately coarse for the MVP — enough to answer "can this user write
  vs. only read vs. also manage membership" without designing a permission
  matrix nobody has asked for yet.
- `projects.owner_user_id` **stays**, even though every owner also gets a
  `project_members` row with `role = 'owner'` (see below). It answers two
  questions membership rows alone can't answer atomically:
  - **Deleting a project.** Restricting delete to the single
    `owner_user_id` avoids a "last owner deletes, second owner is left
    holding a project with no owner" race that a membership table alone
    invites.
  - **"Can the last owner be demoted/removed?"** A column gives one
    unambiguous answer to check without a `COUNT(*) ... WHERE role =
    'owner'` on every membership write.

**Backfill:** every existing project gets one `project_members` row for its
`owner_user_id` with `role = 'owner'`, in the migration itself, so the
invariant "every project has an owner membership row" holds from the moment
this migration lands — not just for projects created after it.

## Consequences

- This migration changes schema only. No handler, middleware, or query used
  by existing code changes, so the existing test suite is unaffected — see
  the issue's "この時点でアプリの挙動は変わらない" requirement, verified by
  running `make test` unchanged.
- `owner_user_id` and the `owner`-role `project_members` row must be kept in
  sync going forward (e.g. transferring ownership must update both). That
  bookkeeping is not needed yet — nothing writes to `project_members` until
  #99 — but whoever adds a "transfer ownership" action later must remember
  both.
- Issue #99 will replace the `owner_user_id`-equality check
  (`GetProjectForOwner` and friends) with a membership check. Because this
  migration guarantees every project already has an `owner` membership row,
  that follow-up can switch the read path over without a second backfill
  step.
- Three roles may prove too coarse once real usage shows up (e.g. a
  "can manage GitLab connection" permission distinct from "can write
  tasks"). That is deferred, not answered here, the same way #0008 deferred
  what a non-owner may do with a shared GitLab token.
