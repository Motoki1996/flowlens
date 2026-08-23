# Contributing to FlowLens

Thanks for taking the time. FlowLens is a task tracker that keeps its tasks
1:1 with GitLab CE issues, and a delivery-flow view built on the merge
request and CI data it syncs.

## Getting set up

```bash
git clone https://github.com/Motoki1996/flowlens.git
cd flowlens
make setup     # installs Go + npm dependencies, creates .env
make dev       # Postgres + API + web, hot reload, http://localhost:4000
```

`make setup` copies `.env.example` to `.env`, which needs an
`ENCRYPTION_KEY` before the API will start:

```bash
echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
```

The API applies its own migrations on startup, so there is no separate
migrate step for a fresh database.

Working inside the devcontainer? Docker is not available in there — use
`make dev-container`, which runs the API and web natively against the
sibling `db` service. It also pre-requests the common web routes in the
background so Next.js has compiled them before you open the first screen
(`FLOWLENS_DEV_WARM=0` opts out).

The devcontainer keeps the Go build cache, the gopls index, the
golangci-lint cache, the Playwright browsers and the npm cache in named
volumes, so a container rebuild does not throw them away. The first rebuild
after this was introduced still starts from empty — the volumes are new.

## Before you open a pull request

```bash
make test        # Go + web unit tests
make lint        # golangci-lint + ESLint
```

`make test-integration` and `make test-e2e` additionally need a running,
migrated Postgres. CI runs all four.

## Conventions worth reading first

These are not style preferences; they are what keeps the codebase coherent,
and a change that ignores them will be asked to change.

| Topic | Document |
| --- | --- |
| Architecture and layering | [`docs/architecture.md`](docs/architecture.md) |
| Web UI design (object-first, not task-first) | [`docs/ui-design.md`](docs/ui-design.md) |
| Testing strategy and rules | [`docs/testing.md`](docs/testing.md) |
| Storybook conventions | [`docs/storybook.md`](docs/storybook.md) |
| Why things are the way they are | [`docs/decisions/`](docs/decisions/) |

A few that catch people out:

- **Screens are designed object-first.** Extract the domain object, decide
  collection view vs single view, then lay it out — never the reverse. See
  [ADR-0006](docs/decisions/0006-why-ooui.md).
- **Regenerate after touching SQL.** Editing a `*.up.sql` schema file or
  anything in `internal/database/queries` means running `make generate`;
  `internal/database/db` is generated and never hand-edited.
- **Every migration needs its `.down.sql`.** A test enforces the pairing,
  because the documented rollback path depends on it.
- **Migrations stay backward compatible.** Self-hosted upgrades are
  "pull and restart" (see [`docs/self-hosting.md`](docs/self-hosting.md)),
  so a new column is nullable or defaulted, and a removal is split across
  two releases: deprecate, then drop.
- **All GitLab calls go through `internal/gitlab`'s client interface**, so
  tests can use its `FakeClient`.
- **Fakes over mocks**, table-driven cases. See `docs/testing.md`.

## Cutting a release

Releases are tag-driven and maintainer-only; the procedure, including the
clean-install check that has to happen *before* the tag, is
[`docs/releasing.md`](docs/releasing.md).

## Reporting a security issue

Please do not open a public issue — see [`SECURITY.md`](SECURITY.md).

## Licence

By contributing you agree that your contribution is licensed under the
Apache License 2.0, the same terms as the project ([`LICENSE`](LICENSE)).
