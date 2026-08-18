# Security Policy

## Reporting a vulnerability

Please report security issues privately, **not** as a public GitHub issue.

Use [GitHub's private vulnerability reporting][advisory] on this repository
(Security → Report a vulnerability). Include what you did, what happened,
and the version you were running (`GET /version`, or `docker run --rm
ghcr.io/motoki1996/flowlens-api:<tag> version`).

[advisory]: https://github.com/Motoki1996/flowlens/security/advisories/new

You can expect an acknowledgement within a week. Fixes ship as a new
release, with the advisory published once self-hosters have had a chance to
upgrade.

## Supported versions

Fixes land on the latest release. There are no long-term support branches,
so upgrading is the supported remediation.

## What FlowLens holds

If you are assessing the impact of an issue, these are the things worth
protecting:

- **GitLab access tokens**, per project. Encrypted at rest with AES-256-GCM
  under `ENCRYPTION_KEY` ([ADR-0008](docs/decisions/0008-why-per-project-gitlab-connection.md)).
- **Webhook secrets**, encrypted the same way.
- **Project API tokens** for AI agents and integrations. Stored hashed,
  scoped to one project and a fixed route allowlist
  ([ADR-0009](docs/decisions/0009-why-project-scoped-api-tokens.md)).
- **Sessions**, opaque and server-side; only the SHA-256 hash of the cookie
  token is stored.
- **Passwords**, bcrypt.

## Hardening a self-hosted instance

The defaults are tuned for a first run, not for exposure. Before putting an
instance on a network you do not control, see the "Hardening" section of
[`docs/self-hosting.md`](docs/self-hosting.md). In short:

- Serve it over HTTPS. `APP_ENV=production` marks the session cookie
  `Secure`, so the app does not work over plain HTTP by design.
- Set `ALLOW_SIGNUP=false` once your accounts exist.
- Set `TRUSTED_PROXY_HOPS` to match your actual proxy chain — too high lets
  a client forge its address to the rate limiters, too low makes every user
  share one limit.
- Set `GITLAB_CA_CERT_FILE` so GitLab's certificate is verified.
  `GITLAB_TLS_INSECURE_SKIP_VERIFY` defaults to `true` because on-prem
  GitLab CE is usually behind a private CA, and that default is a
  deliberate trade you should undo when you can.
- Back up `ENCRYPTION_KEY`. Losing it makes every stored GitLab token and
  webhook secret permanently unreadable.
