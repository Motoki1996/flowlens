# Storybook guide

> **Status: adopted.** These are the conventions for every new web screen.
> Storybook tooling is installed (see the setup checklist at the end for what's
> still outstanding) — build new screens to satisfy the rules below from the
> start and add stories alongside them.

How we use Storybook for FlowLens' web app (`apps/web`), and how to keep the
story set useful without letting it explode.

Goal — mirroring [`docs/testing.md`](./testing.md): **coverage without bloat.**
A story exists to pin down one meaningful appearance or behaviour. Add roughly
one story per branch a reviewer or designer actually needs to see — not one per
imaginable prop combination.

## What Storybook is for here

- A **living catalog** of every screen and its states, reviewable without running
  the full stack or seeding data.
- A **source of interaction and visual-regression tests** (play functions, and
  later Chromatic or similar), so stories earn their keep as tests, not just docs.

It is **not** a place to re-test business rules — those live in the Go domain and
HTTP layers per `docs/testing.md`. Stories test *rendering and interaction*.

## The one principle everything else depends on: split RSC from presentation

`apps/web` is App Router with React Server Components by default; data is fetched
server-side (`getCurrentUser()`, `lib/api.ts` forwarding the session cookie).
Storybook runs in the browser and **cannot render RSC or import server-only
modules**. So:

- Split each screen into a **data component** (the Server Component that fetches)
  and a **presentational component** that takes plain props and renders.
- **Stories target the presentational component.** Permission and data-state
  branches then become trivial: swap the props.

This is the same seam that keeps the code testable in general — it just makes
Storybook possible as a side effect. If a screen can't be storied, that's usually
a signal its fetching and rendering aren't separated yet.

## Which screens exist is decided elsewhere

Storybook covers the *states* of a screen. *Which* screens exist and what each
one is about is decided by [`docs/ui-design.md`](./ui-design.md) — objects
first, a collection view and a single view per object. Before writing stories,
the screen should already be identifiable as "the collection of X" or "a single
X"; the branches below are then the states of that view.

## What every screen must have

1. **At least one story per screen** — the default, happy-path render.
2. **One story per meaningful branch** where display differs by permission or data
   state. One branch = one story, named for the branch.
3. **The async three-states, where the screen fetches:** `Loading`, `Empty`
   (zero results), and `Error`. These are counted as branches too — they are where
   UI breaks most often and are easiest to forget.

## Rules

### 1. Stories target presentational components, not Server Components

Never try to story a Server Component or anything importing `lib/api.ts`. Story
the props-only presentational component. If one doesn't exist yet, extract it.

### 2. One story per branch — don't multiply the matrix

Branches × states × viewports multiplies fast. Cover **representative**
combinations, not the full cross-product. If a variation can be explored with a
control (see rule 5), don't make it a separate story.

### 3. Cover the async three-states explicitly

For any data-driven screen, `Loading` / `Empty` / `Error` are first-class stories,
each fed the props that produce that state. Empty and error rendering is part of
the contract, not an afterthought.

### 4. Mock the network with MSW, never real fetches

Client-side data or interactions that hit the API are mocked with MSW, with each
story declaring the response it assumes (200 / 403 / 500 / empty). This keeps the
`Error` and `Forbidden` stories honest and independent of a running backend.

### 5. Absorb variation with args/controls before adding a story

Cosmetic or single-axis variation (a long username, a count, a label) belongs in
`args` / `argTypes` controls so a reviewer can explore it live. Reserve separate
stories for genuinely distinct states, matching the "table-driven, one line per
case" spirit of `docs/testing.md`.

### 6. Test behaviour with play functions

For interactive Client Components (e.g. `LogoutButton`, `LoginForm`), add a play
function that drives the interaction and asserts the observable result. The story
then doubles as a lightweight interaction test — assert outcomes, not internals.

### 7. Keep the a11y addon on by default

Run the accessibility addon for every story: contrast, missing labels, focus
order. Standard: **a new screen ships with zero a11y violations** in its stories.

### 8. Colocate and name consistently

- Colocate: `Component.stories.tsx` beside `Component.tsx`.
- Name states from a fixed vocabulary so lists stay scannable:
  `Default`, `Loading`, `Empty`, `Error`, `Forbidden`, plus branch-specific names
  (`AdminView`, `NoGitLabConnected`, …).
- Title stories by their place in the app (e.g. `Screens/Dashboard`,
  `Components/AppHeader`). For object views, name the screen after the object
  and its view (`Screens/MergeRequests` for the collection,
  `Screens/MergeRequest` for the single view) so the story tree reads as the
  object model.

## Relationship to the other docs

They partition, they don't overlap:

| Concern | Where |
| --- | --- |
| Which screens exist, what object each is about | [`ui-design.md`](./ui-design.md) |
| Business rules, authz, security invariants | Go domain / HTTP tests |
| Component rendering across states, visual regression | Storybook stories |
| Component interaction (clicks, form flow) | Vitest + Testing Library **or** a story play function — pick one per behaviour, don't test it in both |

The `docs/testing.md` rule stands: **don't verify the same behaviour at two
layers.** If a play function covers a click, a Vitest test shouldn't re-cover it.

## Setup checklist

- [x] Add Storybook for the Next.js / Vite setup used by `apps/web`
      (`@storybook/nextjs-vite`, config in `apps/web/.storybook`).
- [x] Wire the a11y addon and docs addon.
- [x] Add a `make storybook` target (`storybook dev -p 6006`).
- [x] Wire MSW (`msw-storybook-addon`, CSF3 `mswLoader`) so stories can mock
      `fetch` calls to the API per-story via `parameters.msw.handlers`.
- [x] `AppHeader`, `LoginForm`, `SignupForm` have reference stories, including
      play-function interaction tests for the error and pending-submit states.
- [x] Add a CI build step (`storybook-build` job in `web-checks.yml` runs
      `npm run build-storybook`).
- [ ] Run play functions in CI as actual pass/fail tests, not just a build
      smoke test. `@storybook/test-runner` needs a headful/CI-compatible
      Chromium (Playwright); the sandbox this was built in has no root to
      install Playwright's system deps (`libglib-2.0` etc.), so the play
      functions here are verified by `storybook build` succeeding and by
      code review, not by an actual browser run. Wire this in an environment
      that can run Playwright, or add `@storybook/addon-vitest` (needs
      `@vitest/browser` + a Playwright provider).
- [ ] Decide on Chromatic or an alternative for visual regression.
- [ ] Backfill stories for the remaining existing screens (dashboard).
