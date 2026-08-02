# 0009. Why API tokens act as the project owner, with a two-tier scope enforced in HTTP middleware

- **Status:** Accepted
- **Date:** 2026-08-02

## Context

Issues #66 and #68 built the AI-facing task context API: an external
integration (typically an AI coding agent) needs to read and write one
project's tasks without a user session. The mechanism is a project-scoped
bearer token (`internal/apitoken`), but three design questions had to be
settled before any handler could use it:

1. **How does a bearer-authenticated request get authorized against the
   rest of the app?** Every domain service (`task.Service`,
   `backlog.Service`, `taskdependency.Service`, …) already scopes its
   queries by `owner_user_id` — "authorization lives in SQL, not handlers"
   ([`docs/architecture.md`](../architecture.md) layering rule 3). A token
   needs a second, narrower boundary on top of that: not just "does this
   user own this project" but "does this *token* belong to this project."
2. **How fine-grained should a token's scope be?** The read/write split
   already existed in the schema (`project_api_tokens.scopes`), but nothing
   forced it to stay that coarse — a per-resource model (`tasks:write`,
   `backlogs:read`, …) was on the table.
3. **Should a bearer token be usable from the browser?** The web app already
   sends its session cookie cross-origin with credentials; letting it also
   attach `Authorization` would let a token be tested from the browser
   console, at the cost of exposing it to the app's own CORS surface.

## Decision

**A token resolves to its project owner and acts as them; the project
boundary is a second check layered in HTTP middleware, not in the domain
layer.**

`apitoken.Service.Authenticate` resolves a raw token to an `Auth` carrying
the token's project ID, scopes, and — critically — the project's
`OwnerUserID`. `requireBearerAuth` (`internal/http/bearer_middleware.go`)
puts that owner into the exact same `userContextKey` a session request
uses. Every existing service method, which already enforces
"does this user own this row," now enforces the same check for a
bearer-authenticated caller with zero changes — a bearer request acts as a
real user who happens to own several projects. That alone is not a project
boundary, since a token must be confined to *one* of that owner's projects,
not all of them, so a second layer runs only on the explicit allowlist of
bearer-reachable routes: `requireTokenScope` (rejects a read-only token on a
write route), `requireTokenProjectMatch` (a URL's `{projectID}` must equal
the token's own), and `requireTokenResourceProject` (resolves a
single-resource URL like `{taskID}` to its project first). A mismatch is
reported as 404, identical to a foreign session's "not found" — a token can
never distinguish "not yours" from "does not exist."

The alternative — giving every project-scoped service method a parallel
`ForProject` variant that takes the token's project instead of (or in
addition to) the acting user — was rejected. It would double the surface of
`task`, `backlog`, `taskdependency`, `linkedproject`, and every future
project-scoped package, each needing its own project-mismatch test on top of
its existing ownership test. Worse, it would pull `apitoken.Auth` — an
HTTP-adjacent, transport-level credential — into the domain layer, which
`docs/architecture.md`'s first layering rule (domain packages know nothing
about transport) exists specifically to prevent.

**Scopes stay two-tier: `read` and `write`, with `write` implying `read`.**
The AI-facing use case is "let one agent read and update its own project's
tasks," not "let an agent touch tasks but not backlogs." A resource-level
scope model multiplies the routes × scopes matrix that the token-issuance
UI, `requireTokenScope`, and the HTTP tests all have to track, for a
distinction no caller has asked for. Because every bearer route already
declares its own required scope explicitly
(`requireTokenScope(apitoken.ScopeWrite)`), and `apitoken.normalizeScopes`
already validates against a fixed vocabulary, a third scope can be added
later without restructuring anything that exists today.

**`Authorization` is never added to the web app's CORS-allowed request
headers.** The web app authenticates purely by session cookie; a bearer
token is for direct, server-to-server calls — an agent's HTTP client, a
`curl` script, a CI job — never a browser tab. Leaving `Authorization` out
of `Access-Control-Allow-Headers` means the browser's own CORS preflight
refuses any cross-origin script that tries to attach one, at zero
implementation cost: even a future XSS in the web app cannot use a bearer
token cross-origin, because the browser itself won't let a `fetch` attach
it. This is enforced by the browser, not by an app-level policy that code
could get wrong.

This extends [ADR-0008](0008-why-per-project-gitlab-connection.md): the
GitLab connection is already scoped to the app project rather than to a
login, because the trust boundary FlowLens actually cares about is "this
project." An API token continues the same idea one level up the stack — the
credential it grants is also "this project" — which is exactly why
`requireBearerAuth` can resolve it straight to the project's *owner* and
reuse every owner-scoped service method unmodified.

## Consequences

- A new bearer-reachable route must be added to the explicit allowlist in
  `server.go`'s second `api.Group`, with the correct
  `requireTokenScope`/`requireTokenProjectMatch`/`requireTokenResourceProject`
  middleware attached by hand. There is no domain-layer backstop — forgetting
  the project-match middleware would let a token reach another project owned
  by the same user. This is mitigated by the allowlist being small, reviewed
  by hand, and covered by an HTTP-layer test per route
  ([`docs/testing.md`](../testing.md)), not by any structural guarantee.
- Because scopes are coarse, a `write` token can write everything in its
  project (tasks, backlogs, dependencies) — there is no way to issue a token
  that can only touch one backlog or one task. Acceptable while the use case
  is one agent per project; revisit if a use case needs a smaller blast
  radius than "the whole project."
- A revoked, expired, or unknown token fails the same way — 401, no
  distinguishing detail — consistent with how a bad session and a bad GitLab
  token already behave elsewhere in the app.
- Keeping `Authorization` out of CORS means the web app itself can never
  call a bearer-authenticated route with a token; anything the UI needs to
  show goes through a session-authenticated route instead, which already
  covers a superset of what any token can do.
