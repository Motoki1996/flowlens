# Changelog

Notable changes per release. FlowLens follows semantic versioning, and a
release that needs a self-hoster to do anything beyond `docker compose pull
&& docker compose up -d` is marked **⚠️ Breaking** with the steps written
out — see [`docs/self-hosting.md`](docs/self-hosting.md) for the upgrade
procedure itself.

## Unreleased

### Added

- **Self-hosting.** `compose.yaml` pulls prebuilt images from GHCR, so an
  install is that file plus a `.env` with no clone, no toolchain and no
  separate migrate step. `compose.tls.yaml` adds HTTPS via Caddy.
- The API applies its own embedded schema migrations on startup
  (`RUN_MIGRATIONS`, default on).
- `flowlens-api gen-key` prints a ready-to-paste `ENCRYPTION_KEY`, so
  generating one needs nothing but the image itself.
- `GET /version` and `flowlens-api version` report the running build.
- `ALLOW_SIGNUP` closes registration on an instance whose accounts already
  exist. The first account is always allowed, so a fresh instance can be
  bootstrapped with it already off.
- `METRICS_TOKEN` puts `GET /metrics` behind a bearer token.
- `TRUSTED_PROXY_HOPS` tells the per-IP rate limiters how many proxies to
  trust in `X-Forwarded-For`.
- Multi-arch (amd64 + arm64) release images on `ghcr.io`, plus an offline
  image bundle attached to each release for air-gapped installs.
- `LICENSE` (Apache-2.0), `CONTRIBUTING.md`, `SECURITY.md`, issue
  templates, and [`docs/self-hosting.md`](docs/self-hosting.md).

### Changed

- The browser now calls the API on the web app's own origin; the Next.js
  server proxies `/api`, `/auth` and `/webhooks` through to it. This is what
  lets one prebuilt web image serve any hostname, and it removes the CORS
  and cross-site-cookie setup that a separate API origin needed.
  `NEXT_PUBLIC_API_BASE_URL` is still honoured for a separate API origin,
  but is no longer set by default. The proxy destination is
  `API_INTERNAL_URL`, defaulting to the compose service name
  `http://api:8080`; it is resolved at image build time, so renaming that
  service means rebuilding the web image.
- `compose.yaml` no longer publishes the API port. `/healthz`, `/version`
  and `/metrics` are reachable only from inside the Docker network.
- `make build-images` tags images so `compose.yaml` can run them locally.

### Removed

- `docker-compose.prod.yml`, superseded by `compose.yaml`. To run
  self-built images, `make build-images` then
  `FLOWLENS_VERSION=dev docker compose -f compose.yaml up -d`.
