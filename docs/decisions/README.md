# Architecture Decision Records

This directory holds Architecture Decision Records (ADRs) — short documents
that capture a significant decision, its context, and its consequences.

- Use [`0000-template.md`](0000-template.md) as the starting point.
- Number ADRs sequentially and never renumber existing ones.
- Supersede rather than delete: mark an old ADR as superseded and link to
  the new one.

## Index

| ADR | Title | Status |
| --- | ----- | ------ |
| [0001](0001-why-go-and-nextjs.md) | Why Go and Next.js | Accepted |
| [0002](0002-why-rest-api.md) | Why a REST API | Accepted |
| [0003](0003-why-postgresql.md) | Why PostgreSQL | Accepted |
| [0004](0004-why-monorepo.md) | Why a monorepo | Accepted |
| [0005](0005-why-manual-sync-first.md) | Why manual sync for the first MVP | Accepted |
| [0006](0006-why-ooui.md) | Why object-oriented UI (OOUI) | Accepted |
| [0007](0007-why-outbox-worker.md) | Why a Postgres outbox and in-process worker for GitLab sync | Accepted |
| [0008](0008-why-per-project-gitlab-connection.md) | Why the GitLab connection is per app project, and tasks link 1:1 to issues | Accepted |
