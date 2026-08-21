# Releasing FlowLens

The maintainer's procedure for cutting a release. The self-hoster's side of
the same event is [`docs/self-hosting.md`](self-hosting.md).

A release is tag-driven: pushing a `v*` tag runs
[`.github/workflows/release.yml`](../.github/workflows/release.yml), which
builds multi-arch images, pushes them to GHCR, creates the GitHub Release
and attaches an offline image bundle. The version in the image, the git tag
and the release notes are the same string by construction, so there is
nothing to keep in sync by hand.

## Before the tag

**1. Settle the CHANGELOG.** Rename `## Unreleased` to `## vX.Y.Z — <date>`
and open a fresh empty `## Unreleased` above it. Mark anything that needs a
self-hoster to do more than `docker compose pull && docker compose up -d`
as **⚠️ Breaking**, with the steps written out.

**2. Run the checks.**

```bash
make test
make lint
```

**3. Verify a clean install, the way a self-hoster does it.** This is the
step that earns its keep — it catches what CI cannot, because CI never
assembles the distributed artifact:

```bash
make build-images

rm -rf /tmp/flowlens-clean && mkdir -p /tmp/flowlens-clean && cd /tmp/flowlens-clean
cp "$REPO/compose.yaml" .
cp "$REPO/.env.example" .env
sed -i '' 's/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=localtest/' .env
sed -i '' 's/^FLOWLENS_VERSION=.*/FLOWLENS_VERSION=dev/' .env
docker run --rm ghcr.io/motoki1996/flowlens-api:dev gen-key >> .env
docker compose up -d
```

Then check all four:

- `docker compose ps` — three containers up, and `web` is the only one with
  a published port.
- `docker compose logs api` — a migration line, then `api listening`.
- `docker compose exec api /app/api version` — reports the build.
- Register an account at <http://localhost:4000> **in Chrome**. `compose.yaml`
  sets `APP_ENV=production`, which marks the session cookie `Secure`; Chrome
  treats `http://localhost` as a secure context and Safari does not, so
  Safari will look like a broken login when nothing is wrong.

If port 4000 is taken (a dev container, another stack), move the test
install rather than the thing already running — and move `APP_PUBLIC_URL`
with it, since that is the base URL for cookies and generated links:

```bash
sed -i '' 's/^FLOWLENS_PORT=.*/FLOWLENS_PORT=4100/' .env
sed -i '' 's|^APP_PUBLIC_URL=.*|APP_PUBLIC_URL=http://localhost:4100|' .env
```

Note that an editor's port forwarding (VS Code forwards a dev container's
ports to the host automatically) can claim the host port ahead of Docker and
swallow the request, which presents as a page that loads forever rather than
as an error.

## Cutting the tag

```bash
git checkout main && git pull
git tag -a vX.Y.Z -m "FlowLens vX.Y.Z"
git push origin vX.Y.Z
```

Watch it: `gh run watch $(gh run list --workflow=release.yml -L1 --json databaseId -q '.[0].databaseId')`.
A multi-arch build takes roughly 20 minutes, because arm64 is built under
QEMU emulation.

If the workflow fails before anything is pushed to GHCR, the tag can be
moved — nothing has consumed it yet:

```bash
git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z
# fix, merge, then re-tag
```

Once images *are* published, do not move a tag. Cut the next patch version
instead.

## After the tag

**Write the release notes.** The workflow creates the Release and attaches
the artifacts, but leaves the body empty. Paste the CHANGELOG section for
this version, with an install snippet above it pinned to the new tag.

Note the workflow attaches `.env.example` as `default.env.example`: GitHub
rejects an asset name beginning with a dot and renames it. Nothing links to
that asset by name, so it is cosmetic.

**First release only — make the GHCR packages public.** A package created
by `GITHUB_TOKEN` is private by default *even when the repository is
public*, and `compose.yaml` pulls anonymously. Miss this and every
self-hoster gets `Error response from daemon: error from registry: denied`
on `docker compose up`. There is no `gh` command for it; it is the web UI:

- <https://github.com/users/Motoki1996/packages/container/flowlens-api/settings>
- <https://github.com/users/Motoki1996/packages/container/flowlens-web/settings>

On each: **Danger Zone → Change visibility → Public**. The `flowlens`
repository is already linked with the `Admin` role, granted automatically to
the repository whose workflow created the package, so Actions access needs
nothing.

**Verify the published path.** Everything up to here was tested against
locally built `:dev` images; nobody has yet pulled what was actually
published. Log out first, or your own credentials will make a private
package look public:

```bash
docker logout ghcr.io
cd /tmp/flowlens-clean && docker compose down -v
sed -i '' 's/^FLOWLENS_VERSION=.*/FLOWLENS_VERSION=vX.Y.Z/' .env
docker compose pull && docker compose up -d
docker compose exec api /app/api version   # must print vX.Y.Z, not a git describe
```

**Refresh the docs' example version.** `docs/self-hosting.md` uses a
concrete tag in its upgrade, mirroring and offline-bundle snippets. A
version that does not exist gives a copy-paster `manifest unknown`.

## What CI does and does not cover

`release.yml` runs on a tag alone, so no pull request ever exercises it —
a mistake in that file surfaces for the first time on a real tag. v0.1.0
failed this way: `github.repository_owner` keeps the account's original
casing (`Motoki1996`) and a Docker repository name must be lowercase, which
`compose.yaml` hid by hardcoding the lowercase form.

`web-checks.yml` runs a production `next build` for a related reason: plain
`tsc` accepts a route file that Next.js rejects, and without that job the
failure would land inside the release image build instead.
