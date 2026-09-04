# Accounts, members and notifications

## Changing your password

Every account can change its own password from **Settings → Password**, or
directly against the API. The route is session-only: a project API token
can never call it, since a token must not be able to take over the account
it acts as (see [What a token can't reach](agents.md#what-a-token-cant-reach)).

```bash
curl -X PUT "$API_BASE_URL/api/v1/me/password" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -d '{"currentPassword": "…", "newPassword": "…"}'
```

A successful change (`204`) **revokes every session the user holds** and
issues a fresh one in the same response. Changing a password is what
someone does when they think a session of theirs is in the wrong hands, so
no older token survives it — including the one that made the call, which is
replaced rather than kept, so the caller stays signed in.

There is no password-reset email flow: FlowLens has no mail transport and
targets closed networks. An account whose password is lost is recovered by
an operator, with `flowlens-api hash-password` — see
[Recovering a lost password](../self-hosting.md#recovering-a-lost-password).

## Project membership

A project can have more than one user, each with a role — `owner`,
`member`, or `viewer` — recorded in `project_members`
([ADR-0010](../decisions/0010-why-project-membership.md)). Adding,
changing a role, and removing a member are owner-only; **listing members
is open to any project member**, since a Backlog/Epic/Task assignee picker
needs the list regardless of the caller's own role. Every one of these
routes is session-only (a project API token can never reach them — see
[What a token can't reach](agents.md#what-a-token-cant-reach)).

Adding a member this way resolves an **existing** FlowLens account by
username or email. For someone who has no account yet — the normal case on
an instance with `ALLOW_SIGNUP=false` — use an
[invite link](#invite-links) instead.

```bash
# Add an existing user by username or email
curl -X POST "$API_BASE_URL/api/v1/projects/$PROJECT_ID/members" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -d '{"identifier": "octocat", "role": "member"}'

# List members
curl "$API_BASE_URL/api/v1/projects/$PROJECT_ID/members" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE"
```

```jsonc
// GET /api/v1/projects/{projectID}/members
[
  {
    "userId": "b7e1...",
    "username": "octocat",
    "displayName": "Octo Cat",
    "role": "member",
    "isProjectOwner": false,
    "createdAt": "2026-08-02T00:00:00Z"
  }
]
```

The response never includes email — this endpoint accepts a username or
email to invite, but returning one back would let an owner use it to
enumerate registered accounts. `isProjectOwner` marks the row belonging to
the project's single designated owner (`projects.owner_user_id`); `role`
alone cannot identify them, since any number of members may hold the
`owner` role.

To find someone without knowing their exact identifier, the invite form
searches candidates as you type:

```bash
# Owner-only, session-only; q shorter than 2 characters returns []
curl "$API_BASE_URL/api/v1/projects/$PROJECT_ID/member-candidates?q=octo" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE"
```

```jsonc
// GET /api/v1/projects/{projectID}/member-candidates?q=octo
[{ "userId": "b7e1...", "username": "octocat", "displayName": "Octo Cat" }]
```

The candidate set is deliberately narrow: only users the caller **already
shares some project with**, minus the caller and minus this project's
existing members, capped at 10 hits. Email is neither matched nor returned.
FlowLens has no tenant boundary, so a general user-search endpoint would
hand every signed-up account a directory of every other account — the same
enumeration risk that keeps email out of the member list. Anyone outside
that set can still be invited through `POST .../members`, which is
unchanged: you just have to know their exact username or email.

Three invariants apply to `PATCH`/`DELETE .../members/{userID}`, all
returning `400`: you cannot change your own role, you cannot remove
yourself, and the designated owner can neither be demoted nor removed.
These routes manage *other* people's access — without the self-removal
rule a co-owner could delete their own membership and lock themselves out
of a project they have no way back into. There is deliberately no "leave
project" action yet; if one is added it will be its own endpoint. The web
app applies the same rules ahead of time: in the Project view's Members
section, your own row and the designated owner's render as a plain role
badge with no controls.


## Invite links

An invite is a single-use link that lets someone with **no FlowLens account
at all** create one and join a project. It exists because the two halves of
onboarding otherwise contradict each other: adding a member needs the person
to be registered already, and [`docs/self-hosting.md`](../self-hosting.md)
tells you to close registration with `ALLOW_SIGNUP=false`. An invite reopens
that door for one named person, once, instead of reopening it for everyone.

From the project's single view, open the **Invites** card, click **Create
invite**, pick a role and an expiry, and copy the link — shown exactly once,
like an API token, because only its SHA-256 hash is stored. **FlowLens sends
no email**: it has no mail transport and targets closed networks, so you
hand the link over yourself.

```bash
# Owner-only, session-only.
curl -X POST "$API_BASE_URL/api/v1/projects/$PROJECT_ID/invites" \
  -H "Content-Type: application/json" \
  -H "Cookie: flowlens_session=$SESSION_COOKIE" \
  -H "X-CSRF-Token: $CSRF_TOKEN" \
  -d '{"role": "member", "expiresInDays": 7}'
```

```jsonc
{
  "id": "b7e1...",
  "projectId": "a1b2...",
  "role": "member",
  "tokenPrefix": "fli_9f3a2c1d",
  "status": "pending",
  "expiresAt": "2026-08-27T00:00:00Z",
  "createdAt": "2026-08-20T00:00:00Z",
  "token": "fli_9f3a2c1d8e2b4a1f6c3d5e7a9b0f1c2d" // only ever present here
}
```

The invitee opens `/invites/<token>`:

- **No account yet** — they get a sign-up form. That signup is exempt from
  `ALLOW_SIGNUP`, and creates the account *and* the membership together.
- **Already signed in** — they get a "Join project" button
  (`POST /api/v1/invites/accept`).

Whichever path, the invite is spent: a link admits exactly one person, and
`role` decides what they get. `GET .../invites` lists them (including the
accepted and expired ones, so you can see who was let in) and
`DELETE /api/v1/invites/{inviteID}` revokes one.

Everything that can go wrong on the acceptance path — unknown token,
expired, already used — is reported identically, so whoever holds a link
cannot probe which invites ever existed.
## Notification digest (issue #109)

Overdue tasks and sync failures used to require an actual `/dashboard`
visit to notice. A background worker now sends a daily digest per project by
**outgoing webhook** — chosen over email because it needs no SMTP setup and
plugs straight into Slack via an Incoming Webhook URL (agreed on the issue
before implementation).

- `PUT`/`GET /api/v1/projects/{projectID}/notification-settings` (session,
  owner-only — the webhook URL is an outbound destination a lesser role
  should not be able to redirect, the same reasoning `gitlab-connection`
  applies to its credential): `{ "webhookUrl": string, "enabled": boolean,
  "sendHour": 0-23 }`. `GET` on a project that has never configured
  notifications returns the unconfigured defaults (`enabled: false`,
  `sendHour: 9`) rather than 404 — settings conceptually always exist.
- A background worker (`internal/notification.Worker`, same process as the
  sync workers, gated by `SYNC_WORKER_ENABLED`) sweeps every enabled project
  roughly every 15 minutes. Once a project's `sendHour` (UTC) has been
  reached, it builds that project's digest: open tasks overdue, open tasks
  due today (the finest-grained "within 24h" `due_on`'s `DATE` type
  supports), and failed `sync_jobs` / `webhook_events`. **A digest with
  nothing to report is never sent.**
- Sending is logged to `notification_digests` (`project_id`, `digest_date`)
  *before* the webhook POST, and that table's `UNIQUE (project_id,
  digest_date)` constraint is the whole dedupe guard: a second sweep the
  same day (or a second process) hits the constraint and skips instead of
  double-sending. A day with nothing to report never rows there at all, so
  it doesn't block a later same-day sweep once something *does* need
  reporting.

