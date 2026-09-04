# GitLab CE connection & sync

Every FlowLens `Project` may connect to a GitLab CE instance and link one or
more GitLab projects to it. Once linked, tasks and GitLab issues are kept in
sync in both directions. This section is everything you need to go from a
fresh FlowLens project to a completed first import.

The connection (base URL + personal access token) is stored **per FlowLens
project, not per user** — see [ADR-0008](../decisions/0008-why-per-project-gitlab-connection.md)
for why. Every project member who can reach the project shares the same
GitLab credentials; there is no per-user token for issue sync.

## Generating ENCRYPTION_KEY

The API refuses to start without `ENCRYPTION_KEY` — a base64-encoded 32-byte
AES-256 key used (via `internal/crypto`) to encrypt the GitLab access token
and every webhook secret before they're written to the database. Generate
one with:

```bash
openssl rand -base64 32
```

The API never returns a stored access token in a response — only its last
four characters — so losing `ENCRYPTION_KEY` means re-entering every
project's GitLab token, not losing data silently.

## Creating a GitLab personal access token

In your GitLab CE instance: **Profile → Access Tokens** (or, for a bot
account dedicated to FlowLens, that account's own access tokens).

- **Scope:** `api` — FlowLens both reads issues (initial import) and writes
  them (create/update/close/reopen), so the read-only `read_api` scope is
  not enough.
- **Role on each project you plan to link:** at least **Maintainer**.
  FlowLens registers a webhook on your behalf, and GitLab CE only allows
  Maintainer+ to manage a project's webhooks — a lower role saves the
  connection but fails to register any webhook
  (`gitlabconn.ErrInsufficientScope` / `linkedproject.ErrWebhookForbidden`,
  surfaced in the UI as a failed/unverified connection or webhook).

Paste the base URL of your GitLab CE instance (e.g.
`https://gitlab.example.com`) and the token into the project's GitLab
connection form (`PUT /api/v1/projects/{projectID}/gitlab-connection`), then
use **Test connection** (`POST .../gitlab-connection/test`) to confirm
FlowLens can reach the instance and the token is both valid and sufficiently
scoped before linking any project. **Disconnect**
(`DELETE .../gitlab-connection`) removes the stored token and, with it, every
project linked through the connection; the tasks themselves stay, as local
tasks.

## TLS for a self-hosted instance

An on-prem GitLab CE typically presents a certificate signed by a private CA
(or a self-signed one) that the API container has no reason to trust. Go
verifies against the system roots by default, so the handshake fails before
any request is sent and the connection form can only report it as
`unreachable`. Two environment variables control this, and they apply to
**GitLab only** — the client's TLS policy is passed in at wiring time
(`gitlab.TLSPolicy`), so a future GitHub client keeps verifying normally.

| Variable | Default | Effect |
| --- | --- | --- |
| `GITLAB_TLS_INSECURE_SKIP_VERIFY` | `true` | Skips certificate verification entirely. Only safe on a network you already trust — it leaves the connection open to interception. The API logs a warning at startup while it is on. |
| `GITLAB_CA_CERT_FILE` | *(unset)* | Path to a PEM bundle added to the system roots. **Takes precedence**: naming a CA turns verification back on, so a configured CA can never silently degrade into no verification. |

Preferring the CA file is the better setup wherever the CA is available:

```bash
# docker-compose.yml already reads both; mount the CA into the api service.
GITLAB_CA_CERT_FILE=/etc/ssl/certs/corp-ca.pem
```

A certificate the policy still rejects is reported distinctly from a network
failure — 422 `tls_error` rather than `unreachable`, with the underlying
x509 error in the API log — and is not retried, since retrying only repeats
the same handshake.

## Linking a GitLab project and the initial import

From the project's GitLab connection, list the GitLab projects the token can
see (`GET .../gitlab-connection/available-projects`) and link one
(`POST .../linked-gitlab-projects`). Linking:

1. Registers a webhook on the GitLab project (see below), if
   `APP_PUBLIC_URL` is configured.
2. Enqueues an initial import (`gitlab_sync_runs.kind = 'initial_import'`)
   that pulls every in-scope issue and creates a task for each one that has
   no existing link — the reverse of a normal push, since these issues
   already existed before FlowLens knew about them. Imported issues land
   with no backlog (Unclassified / unfiled) so they can be triaged afterwards.

The first project you link becomes that FlowLens project's **default**
linked GitLab project — new tasks with no explicit link are pushed there.
Any other link can be promoted to default afterwards from its own view
(`PATCH /api/v1/linked-gitlab-projects/{linkID}` with `isDefault: true`);
there is no way to leave a connection with no default at all. A single link
reads back at `GET /api/v1/projects/{projectID}/linked-gitlab-projects/{linkID}`
— project-nested, unlike the flat `PATCH`/`DELETE`, because the link itself
carries no project in its response. Trigger a re-sync at any time from the
linked project's view (`POST .../sync-runs`, `kind = 'manual_resync'`).

## A backlog's own GitLab project

A project's default link is project-wide, but a team often keeps one backlog's
work in a different GitLab repository. A backlog can therefore name its own
destination for new issues — `defaultLinkedGitlabProjectId` on
`POST /api/v1/projects/{projectID}/backlogs` and
`PATCH /api/v1/backlogs/{backlogID}`, editable from the create/edit form on the
Backlog collection screen and shown on the Backlog single view.

Creating a task resolves where its GitLab issue goes in this order:

1. the task's backlog's own linked GitLab project, if it names one;
2. otherwise the project's **default** linked GitLab project;
3. otherwise nowhere — the task stays purely local.

Two rules make this predictable:

- **It is read only at task-create time.** Once an issue exists, its linked
  project is recorded in `task_gitlab_links` and every later update, close and
  reopen follows that row, so moving a task between backlogs afterwards never
  moves the issue or re-targets it. FlowLens does not move GitLab issues
  between projects.
- **The link must belong to the same FlowLens project.** A link from another
  project is rejected with 400 `invalid_linked_gitlab_project`. Unlinking a
  GitLab project doesn't delete the backlogs that named it — they fall back to
  the project default (`ON DELETE SET NULL`).

Inbound sync is unchanged: an issue imported or delivered by webhook still
lands with no backlog (Unclassified), whichever GitLab project it came from.

## What FlowLens registers as a webhook

For each linked GitLab project, FlowLens registers exactly one webhook on
GitLab (GitLab project → **Settings → Webhooks**):

| Field | Value |
| --- | --- |
| URL | `{APP_PUBLIC_URL}/webhooks/gitlab/{linkedGitlabProjectID}` |
| Events | **Issues events** and **Comments events** only — no push, merge request, pipeline, or other events are requested |
| Secret token | A random 32-byte value FlowLens generates per link, encrypted at rest, and never re-displayed. GitLab sends it back as `X-Gitlab-Token` on every delivery, and the receiver compares it in constant time (`hmac`-style, not `==`). |

Repairing or re-registering the webhook (e.g. after it was deleted on
GitLab) is idempotent: FlowLens lists the project's existing hooks first and
updates the one at its own URL rather than creating a duplicate
(`POST /api/v1/linked-gitlab-projects/{linkID}/webhook`).

Every delivery is recorded, and the link's own view lists them newest first
for troubleshooting (`GET .../webhook-events`, paged with `?page=`/`?per_page=`
— the response's `nextPage` is `0` on the last page — and narrowed with
`?status=`). The listing omits each delivery's raw payload, which is fetched
on demand from `GET .../webhook-events/{eventID}` when a row's payload is
opened; a failed delivery can be re-applied with
`POST .../webhook-events/{eventID}/retry`.

## Task comments sync with GitLab issue discussions

A task's activity log (see "Activity log (comments)" below) rides the same
outbox/webhook machinery as its title, description and status: posting a
comment on a task linked to GitLab enqueues a `comment.create` job that posts
it to the issue as a GitLab note (`POST .../issues/:iid/notes`), and GitLab's
own `Note Hook` webhook deliveries are applied back onto the task as
`authorKind: "gitlab"` comments. A task with no GitLab link behaves exactly
as before — nothing is pushed, and no `"gitlab"`-authored comments ever
appear.

Loop prevention works differently here than for a task's own fields: because
a task can have many independently-created comments, there is no single
per-task content fingerprint to compare against (unlike
`task_gitlab_links.last_pushed_fingerprint`). Instead each `task_comments`
row stores the GitLab note id it was pushed as (or mirrored in from), and an
inbound note whose id already exists on some comment is recognized and
skipped as FlowLens's own echo rather than re-imported. GitLab-generated
system notes (e.g. "changed the description") and notes on anything other
than the linked issue are ignored.

A push failure is retried with the same backoff and eventual dead-letter
(`sync_jobs.status = 'failed'`) as every other outbound job — see "GitLab
CE connection & sync" retry/troubleshooting above.

## Editing a task's assignee and labels

A task's assignee and labels are edited from the task's single view and
mirrored to its GitLab issue through the same outbox worker as every other
field (`issue.update`). Since a GitLab assignee is a GitLab user ID, the web
app looks up candidates from the task's project's **default** linked GitLab
project:

- `GET /api/v1/linked-gitlab-projects/{linkID}/members` — the linked GitLab
  project's members, for the assignee picker (`?search=`, `?page=`/`?per_page=`).
- `GET /api/v1/linked-gitlab-projects/{linkID}/labels` — the linked GitLab
  project's existing labels, for the label picker. Labels not already on
  GitLab can still be typed in freely; they are pushed to GitLab like any
  other label on the next sync.

Both are session-only (not on the project API token's read/write allowlist —
only the web app's editing UI needs them). A project with no GitLab
connection, or no linked GitLab project yet, has no candidates to fetch: the
web app falls back to free-text entry for both fields, and the assignee field
carries no GitLab user ID until the project is connected.

## Sync scope

Each linked GitLab project has its own sync scope, set when linking and
changed later from the link's own view
(`PATCH /api/v1/linked-gitlab-projects/{linkID}`, which rewrites the scope
wholesale rather than patching single fields):

- **`all`** — every issue in the GitLab project is synced.
- **`labels`** — only issues carrying at least one of an explicit label
  list are synced; everything else is ignored on both initial import and
  webhook apply.

## Sync guarantees

- **Task ↔ issue is 1:1.** Enforced by a database `UNIQUE` constraint on
  `(linked_gitlab_project_id, gitlab_issue_iid)`, not just application code —
  two concurrent creates for the same issue cannot produce two tasks.
- **Duplicate deliveries are idempotent.** Every webhook payload carries
  GitLab's `X-Gitlab-Event-UUID`; a second delivery of the same UUID is a
  no-op 200, enforced by `UNIQUE (linked_gitlab_project_id, delivery_uuid)`.
- **Stale events are ignored.** Ordering is decided by GitLab's own
  `updated_at` on the payload, not by arrival order — an event no newer than
  the last state FlowLens applied is skipped rather than overwriting a newer
  local edit.
- **The sync loop cannot feed itself.** Applying an inbound webhook never
  enqueues an outbound push, and an inbound event whose content matches what
  FlowLens itself last pushed is recognized as an echo and skipped — so a
  task edit doesn't bounce back and forth between FlowLens and GitLab.
- **A failed push retries automatically**, with capped exponential backoff,
  from the in-process sync worker (`SYNC_WORKER_ENABLED`,
  `SYNC_WORKER_POLL_INTERVAL`). If every retry is exhausted, the task is
  marked `sync_status: "failed"` with the error visible in the UI and API,
  and can be retried manually (`POST /api/v1/tasks/{taskID}/sync-retry`).
  The same dead-letter jobs are also visible project-wide, in one place,
  from the project's own view (`GET
  /api/v1/projects/{projectID}/sync-jobs?status=failed`, session-only) and
  can be retried directly by job ID (`POST /api/v1/sync-jobs/{jobID}/retry`)
  without opening each affected task in turn.

The design behind these guarantees is recorded in
[ADR-0007](../decisions/0007-why-outbox-worker.md) (the outbox + worker)
and [ADR-0008](../decisions/0008-why-per-project-gitlab-connection.md)
(the per-project connection and the 1:1 link).

## Operating without APP_PUBLIC_URL

`APP_PUBLIC_URL` is the URL GitLab must be able to reach to deliver
webhooks — it only makes sense if your FlowLens instance is reachable from
your GitLab CE instance (i.e. not `localhost` in most setups). When it's
unset:

- Webhook registration is skipped entirely; a linked GitLab project stays in
  `webhookStatus: "not_registered"` rather than failing.
- Outbound sync (FlowLens → GitLab: task create/update/close/reopen) is
  unaffected — it doesn't depend on webhooks at all.
- Inbound sync (GitLab → FlowLens) only happens when you trigger it: use
  **re-sync** on the linked project (`POST
  /api/v1/linked-gitlab-projects/{linkID}/sync-runs`) to manually pull the
  latest state from GitLab. This is a complete substitute for webhooks, just
  not automatic — nothing GitLab-side needs to change.

## GitLab user identity

`user_gitlab_identities` maps the authenticated user to their GitLab user
ID/username on one GitLab CE instance, keyed by `(user_id, gitlab_base_url)`
since GitLab CE is self-hosted and a team may run more than one instance.
It carries no access token — that stays a distinct, still-unbuilt feature
scoped per project ([ADR-0008](../decisions/0008-why-per-project-gitlab-connection.md))
— only the identifiers the `assignee=me` filter above needs to match against
a task's own `assigneeGitlabUserId`.

- `GET /api/v1/me/gitlab-identities` returns every identity the caller has
  registered.
- `PUT /api/v1/me/gitlab-identities` registers or replaces the identity for
  one `gitlabBaseUrl`, given `gitlabUserId` (numeric) and `gitlabUsername`.
  Both session-only, since only the logged-in web user manages their own
  identity. The web app exposes this as a form on `/settings`.
- `assigneeMe` matching is computed entirely in SQL: the task's own
  project's `gitlab_connections.base_url` is joined against the caller's
  registered identity for that same base URL. A project with no GitLab
  connection, or a caller with no registered identity, simply matches
  nothing — never an error.

