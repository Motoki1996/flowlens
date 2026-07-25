# 0006. Why object-oriented UI (OOUI)

- **Status:** Accepted
- **Date:** 2026-07-25

## Context

FlowLens is an exploration tool. A user opens it holding a question about their
delivery flow — "which project's review step is slowest?", "why did this merge
request sit for four days?" — rather than a task to execute. The questions are
open-ended and we cannot enumerate them up front.

The default way a web app grows is task-oriented: each new feature adds a screen
named after an operation ("Sync repositories", "Configure project", "View review
stats"). That scales badly here in two ways. The screen count grows one entry
per feature with no organising structure, and the same object ends up described
differently on each screen it appears on.

We also already carry a naming inconsistency — the database tables use
GitHub-era names (`repositories`, `pull_requests`) while the domain is GitLab —
which is exactly the kind of drift that gets worse without a rule about where
object names come from.

The UI surface is still small (login, signup, dashboard, settings), so the cost
of adopting a convention now is close to zero, and it applies to the project
selection, MR sync, and dashboard screens that are about to be built.

## Decision

Design the web UI object-first, following OOUI: extract the domain objects,
give each a collection view and a single view, and attach actions to the object
they act on rather than building standalone task screens.

The rules, the object model, and a per-screen checklist live in
[`docs/ui-design.md`](../ui-design.md). Authentication flows are an explicit
exemption — they are genuinely task-shaped and stay that way.

## Consequences

**Good**

- The screen set is predictable: it is a function of the object model, not of
  the feature backlog. A new object implies two known screens.
- Navigation between related data (project → merge requests → reviewers) falls
  out of the model instead of being designed per feature.
- It forces one name per object across UI, routes, API and schema, which gives
  us a place to record and eventually resolve the GitHub/GitLab naming drift.
- It pairs naturally with the Storybook convention: collection and single views
  are props-only presentational components, so their states are easy to story.

**Bad / trade-offs**

- Some genuinely linear flows are a poor fit and need the exemption clause. If
  that list grows beyond auth, this decision should be revisited.
- Object extraction is up-front design work before any component is written,
  which feels slower on the first screen of a feature.
- Existing screens (dashboard, settings) do not follow the model yet; they are
  migrated opportunistically, so the codebase is mixed for a while.

**Follow-up**

- Restructure the dashboard around the object model when project selection and
  MR sync land.
- Add `Pipeline` and `Release` to the object model when their tables exist.
