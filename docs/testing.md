# Testing guide

How we test FlowLens, and how to keep the test suite small, fast, and
maintainable as the codebase grows. The current tests already follow these
rules — treat this as the contract for every new test.

Goal: **quality without bloat.** Tests should read like a specification, break
only when behaviour actually changes, and add roughly one new case per new
behaviour — not one new file per new bug.

For the web app, component rendering and visual states are covered by Storybook,
not here — see [`docs/storybook.md`](./storybook.md). The two guides partition the
work and deliberately don't overlap: business rules and contracts in these tests,
component appearance and interaction in stories.

## The layered strategy

Each layer tests only its own concern and trusts the layers below it. The single
biggest cause of an unmaintainable suite is verifying the same behaviour at
several layers, so don't.

| Layer | Location | What it tests | What it must NOT test |
| --- | --- | --- | --- |
| **Database (integration)** | `internal/database/*_test.go`, `//go:build integration` | Real SQL against real PostgreSQL: queries, constraints, upsert idempotency | Business rules, HTTP concerns |
| **Domain (unit)** | `internal/user`, `internal/auth`, … with `dbtest.FakeQuerier` | Business rules: duplicate rejection, password hashing, auth failure, expiry | HTTP status codes, JSON shape |
| **HTTP (unit)** | `internal/http`, through the router | Routing, status codes, cookies, JSON contract, authn/authz | Exhaustive business-rule permutations |

Rule of thumb for where a case belongs:

- Exhaustive permutations of a rule (every invalid password, every duplicate
  case) live in the **domain** layer.
- The **HTTP** layer keeps *one* representative case per branch (e.g. "too-short
  password → 400"), because its job is the wire contract, not the rule.

This keeps the pyramid wide at the bottom (fast fake-backed unit tests) and thin
at the top (a few real-DB integration tests).

## Rules

### 1. Test HTTP handlers through the router, never by calling the function

Always drive handlers with `s.Router().ServeHTTP(rec, req)`, as `handler_test.go`
does. Calling a handler function directly skips the CORS, logging, and auth
middleware — which is exactly where real bugs live. Black-box through the router
is the standard for every handler.

### 2. Prefer Fakes over Mocks

Use the stateful fakes (`dbtest.FakeQuerier`, `gitlab.FakeClient`), not
call-expectation mocks. A fake lets you write natural "create it, then read it
back" assertions and survives refactors. Mocks that assert "method X was called
once with these args" couple tests to implementation and shatter every time the
implementation changes — that is the bloat we are avoiding. When GitLab sync
lands, drive it by seeding responses into `FakeClient`, not by mocking calls.

### 3. Test behaviour and contracts, not implementation

Assert on observable outcomes: status codes, returned DTOs, cookies set/cleared,
sessions revoked. Security invariants get an explicit assertion tied to the
threat model in `CLAUDE.md` — e.g. `assert.NotContains(rec.Body.String(), password)`
so a hash or secret can never leak into a response.

### 4. Use table-driven tests to keep growth linear

When several cases exercise one behaviour with different inputs, collapse them
into a table so adding a case is one line, not one function. Give each row a
`name` and run it under `t.Run` for clear failure output.

```go
tests := []struct {
    name     string
    req      signupRequest
    wantCode int
}{
    {"ok", signupRequest{Username: "octocat", Email: "o@example.com", Password: "hunter22"}, http.StatusCreated},
    {"short password", signupRequest{Username: "octocat", Email: "o@example.com", Password: "short"}, http.StatusBadRequest},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        s, _ := newTestServer(t)
        assert.Equal(t, tt.wantCode, postJSON(t, s, "/auth/signup", tt.req).Code)
    })
}
```

### 5. Push setup into helpers and builders

Shared boilerplate belongs in helpers (`newTestServer`, `loginSession`,
`postJSON`) so each test shows only what makes it different. As fixtures grow,
add seeding helpers (e.g. a `SeedUser` on the fake) rather than repeating
construction. Every helper takes `t` and calls `t.Helper()`.

### 6. Name tests so they read as a spec

`Method_Condition_Expectation`, e.g. `TestHandleLogout_ClearsCookieAndRevokes`.
The name alone should tell a reader what is guaranteed. Use `require` for
preconditions that make the rest of the test meaningless if they fail, and
`assert` for the actual checks.

### 7. Target behaviours, not a coverage percentage

Don't chase a coverage number. Cover each meaningful branch once: authz
boundaries, security invariants, idempotency, and error paths. A green suite
that exercises every important behaviour beats a high percentage that pins down
trivia and resists change.

## E2E (browser)

The three layers above never actually load a page in a browser, so a bug in
how the pieces are wired together — a button that doesn't call the endpoint
its own unit tests assume, a redirect that doesn't fire, cookies that don't
survive a real navigation — can pass all of them and still break the product.
`apps/web/e2e/` (Playwright) exists for exactly that seam, and only that seam.

- **What belongs here:** one smoke test per critical, first-party path — the
  kind of flow that, if broken, means the product doesn't work at all. Today
  that's signup → create project → create task → task shows in the list →
  log out. Add a new e2e case only for another path of that severity, not for
  every screen or every branch.
- **What must NOT be here:** business-rule permutations (domain layer),
  HTTP status/JSON contract checks (HTTP layer), or component
  appearance/interaction states (Storybook, see
  [`docs/storybook.md`](./storybook.md)). If a case can be expressed without a
  real browser, it belongs in a faster layer instead.
- GitLab CE integration (connecting a project, syncing) is out of scope for
  e2e for now — there's no fake GitLab server to run against in this layer,
  only `gitlab.FakeClient` at the domain layer. Revisit if/when that need
  becomes concrete.

Unlike the layers above, e2e tests are **not** part of `make test` — they
need a real, migrated Postgres and start their own API and web servers, so
they get the same treatment as `make test-integration`: a separate command,
run locally against infrastructure you already have up, and in CI as its own
job (`.github/workflows/web-e2e.yml`) with a Postgres service. On failure, CI
uploads the Playwright HTML report (trace, screenshot, video) as an artifact.

## Keeping integration tests independent

Integration tests hit a shared database, so make each run self-contained: derive
unique identifiers per run (see the `time.Now().UnixNano()` suffix in
`integration_test.go`) and clean up what you create. They are gated behind the
`integration` build tag and skipped when `DATABASE_URL` is unset, so the default
`make test` stays fast and hermetic.

## Looking ahead: GitLab sync and dashboards

The same strategy scales to the not-yet-built features:

- All GitLab CE calls go through the `gitlab.Client` interface. Unit-test sync
  logic against `gitlab.FakeClient` with seeded responses — no network, no live
  GitLab.
- Test aggregation/derivation logic (the dashboard metrics) at the domain layer
  with fakes, where permutations are cheap.
- Test idempotency of sync (duplicate webhook deliveries) once at the
  integration layer, where it meets the real `UNIQUE` constraints.

## Commands

| Command | Scope |
| --- | --- |
| `make test` | Go + web unit tests (fast, hermetic, no DB) |
| `make test-integration` | Go integration tests; needs Postgres with migrations applied |
| `make test-e2e` | Playwright browser e2e tests; needs Postgres with migrations applied, starts its own api+web |

Single test:

- Go: `cd apps/api && go test ./internal/auth/ -run TestName`
- Web: `cd apps/web && npx vitest run path/to/file.test.ts -t "test name"`
- Web e2e: `cd apps/web && npx playwright test -g "test name"`
