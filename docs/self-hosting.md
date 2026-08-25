# Self-hosting FlowLens

Everything needed to run FlowLens on your own infrastructure: install,
upgrade, back up, and harden. The only prerequisite is Docker with Compose
— no Go, Node, `psql` or `migrate` on the host.

- [Install](#install)
- [Putting it on a real hostname](#putting-it-on-a-real-hostname)
- [Upgrading](#upgrading)
- [Backup and restore](#backup-and-restore)
- [Adding people](#adding-people)
- [Recovering a lost password](#recovering-a-lost-password)
- [Hardening](#hardening)
- [Closed networks and air-gapped installs](#closed-networks-and-air-gapped-installs)
- [Configuration reference](#configuration-reference)
- [Troubleshooting](#troubleshooting)

## Install

```bash
mkdir flowlens && cd flowlens

curl -O https://raw.githubusercontent.com/Motoki1996/flowlens/main/compose.yaml
curl -o .env https://raw.githubusercontent.com/Motoki1996/flowlens/main/.env.example

# Generate the key that encrypts GitLab tokens at rest. No local toolchain
# needed — the API image does it.
docker run --rm ghcr.io/motoki1996/flowlens-api:latest gen-key >> .env

# Edit .env: set POSTGRES_PASSWORD, and pin FLOWLENS_VERSION to a release.
docker compose up -d
```

Open <http://localhost:4000> and register. **The first account you create is
the one you keep** — set `ALLOW_SIGNUP=false` afterwards if the instance is
reachable by anyone else (see [Hardening](#hardening)).

There is no migrate step: the API carries its schema migrations inside the
binary and applies them on startup.

### What is running

| Service | Port | Notes |
| --- | --- | --- |
| `web` | 4000 (published) | Next.js. The only thing exposed. Proxies `/api`, `/auth` and `/webhooks` through to the API at `http://api:8080`. |
| `api` | 8080 (internal) | Go API and the background sync workers. Not published, so `/healthz`, `/version` and `/metrics` stay off the public origin. |
| `db` | 5432 (internal) | PostgreSQL 16, data in the `db-data` volume. |

### Pin your version

`FLOWLENS_VERSION` defaults to `latest`, which is fine for a first look and
wrong for anything you rely on: with `latest`, a `docker compose pull` you
ran for an unrelated reason can move you across a schema migration you had
not read the notes for. Set it to a release tag and change it deliberately.

## Putting it on a real hostname

`APP_ENV` is `production` in `compose.yaml`, which marks the session cookie
`Secure` — so **FlowLens does not work over plain HTTP on a real hostname**,
by design. You need TLS. The bundled overlay uses Caddy, which obtains and
renews certificates automatically:

```bash
# in .env
APP_PUBLIC_URL=https://flowlens.example.com
FLOWLENS_DOMAIN=flowlens.example.com
TRUSTED_PROXY_HOPS=2      # caddy, then the web service
FLOWLENS_BIND=127.0.0.1   # stop publishing web:4000 to the world

docker compose -f compose.yaml -f compose.tls.yaml up -d
```

Fronting it with a proxy of your own instead is fine — forward everything to
the `web` service on port 4000, and set `TRUSTED_PROXY_HOPS` to the number
of proxies in front of the API (yours, plus 1 for the web service).

`APP_PUBLIC_URL` is also the address GitLab delivers webhooks to, so it must
be reachable from your GitLab instance for inbound sync to work.

## Upgrading

Upgrades are "read the notes, back up, pull, restart". Schema migrations are
kept backward compatible wherever they can be, so the running containers are
replaced in place and the data volume is untouched.

A release that has to drop something says so with a ⚠️ Breaking marker in
the notes, and such a release is **one-way**: the new schema no longer has a
column the older binary reads, so going back needs the database restored
alongside the image (see [Rolling back](#rolling-back)). That is what step 2
below is for.

```bash
# 1. Read the release notes for a ⚠️ Breaking marker.
#    https://github.com/Motoki1996/flowlens/releases

# 2. Back up. Always — a migration is not something you can undo in place.
docker compose exec -T db pg_dump -U flowlens flowlens | gzip > "backup-$(date +%F).sql.gz"
cp .env .env.backup

# 3. Move the pin.
sed -i 's/^FLOWLENS_VERSION=.*/FLOWLENS_VERSION=v0.1.2/' .env

# 4. Apply.
docker compose pull
docker compose up -d

# 5. Confirm what actually came up.
docker compose logs api | tail -20     # look for "database migrations applied"
curl -s http://localhost:4000/api/v1/../version || docker compose exec api /app/api version
```

Keep `.env` — in particular `ENCRYPTION_KEY`. It is not stored in the
database, and without the same key every saved GitLab token and webhook
secret is unreadable.

### Rolling back

**Restore the database alongside the image.** Rolling only the image back
leaves the newer schema in place under a binary that does not know about it.

```bash
docker compose down
sed -i 's/^FLOWLENS_VERSION=.*/FLOWLENS_VERSION=v0.1.1/' .env
docker compose up -d db
gunzip -c backup-2026-08-18.sql.gz | docker compose exec -T db psql -U flowlens flowlens
docker compose up -d
```

This loses everything written since the backup, which is the honest cost of
a rollback and the reason step 2 above is not optional. Do not use the
`.down.sql` migrations for this: they exist for development, and the ones
that drop columns drop the data in them.

## Backup and restore

The whole of your state is the `db-data` volume plus `ENCRYPTION_KEY`.

```bash
# Back up (nightly, from cron)
docker compose exec -T db pg_dump -U flowlens flowlens | gzip > "backup-$(date +%F).sql.gz"

# Restore into an empty database
docker compose up -d db
gunzip -c backup-2026-08-18.sql.gz | docker compose exec -T db psql -U flowlens flowlens
docker compose up -d
```

A backup you have never restored is a guess. Try one into a scratch
instance before you need it.

Running against a managed Postgres instead of the bundled container is
supported and recommended for anything you care about: point `DATABASE_URL`
at it (with `sslmode=require`), and remove the `db` service.

## Adding people

Keep `ALLOW_SIGNUP=false` and add people with **invite links**. Nothing
about onboarding requires reopening registration.

From the project's single view, open the **Invites** card → **Create
invite** → pick a role (`owner`/`member`/`viewer`) and an expiry (7 days by
default, 90 maximum) → copy the link. Send it over whatever channel you
already use.

- The link is shown **once**. Only its hash is stored, so a lost link means
  creating a new invite, not recovering the old one.
- It works **once**. It admits exactly the first person who accepts it, at
  the role you chose.
- The invitee opens it and creates an account from that page — that signup
  is exempt from `ALLOW_SIGNUP`. Someone who already has an account gets a
  "Join project" button instead.
- **No email is sent.** FlowLens has no mail transport; handing over the
  link is your job.
- Revoke an outstanding invite from the same card.

Someone who already has an account can also be added directly by username
or email, from the **Members** card.

## Recovering a lost password

Anyone can change their own password from **Settings → Password** while
they are signed in. There is no reset-by-email flow — FlowLens has no mail
transport, and an air-gapped instance would have nowhere to send to — so an
account that is locked out is recovered by you, against the database.

```bash
# 1. Produce a hash. Read from stdin, so the password never reaches your
#    shell history or the process list. -i is required for that to work.
docker compose run --rm -i api hash-password
# type the new password, press Enter; a $2a$… hash is printed

# 2. Install it.
docker compose exec -T db psql -U flowlens flowlens \
  -c "UPDATE users SET password_hash = '<the hash>' WHERE username = 'someone';"

# 3. Cut any session that account still has open, so a stolen one does not
#    outlive the reset.
docker compose exec -T db psql -U flowlens flowlens \
  -c "DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE username = 'someone');"
```

Tell the person to change it again from Settings once they are back in:
step 2 means you have seen their password, and step 1 enforces only the
same 8-character minimum signup does.

## Hardening

The defaults favour a working first run. Before exposing an instance:

| Do this | Why |
| --- | --- |
| Serve over HTTPS | The session cookie is `Secure` in production; without TLS, login cannot work. |
| `ALLOW_SIGNUP=false` | Otherwise reaching the login page is enough to create an account. Set it after registering; the first account is always allowed, so you can also set it before and still bootstrap. It does not block onboarding — see [Adding people](#adding-people). |
| `TRUSTED_PROXY_HOPS` set to your real chain | The per-IP rate limiters key on it. Too low and every user shares one login limit; too high and a client can forge its address to escape it. |
| `POSTGRES_PASSWORD` changed | It is `change-me` in the example file. |
| `GITLAB_CA_CERT_FILE` pointed at your GitLab's CA | `GITLAB_TLS_INSECURE_SKIP_VERIFY` defaults to `true` because on-prem GitLab CE usually presents a certificate the container cannot verify, and a failed handshake otherwise surfaces only as "unreachable". Undo that trade when you can. |
| `ENCRYPTION_KEY` backed up somewhere other than the host | Losing it is unrecoverable. |
| `METRICS_TOKEN` set, *if* you publish the API port | `compose.yaml` does not publish it, so `/metrics` is already unreachable from outside. This is only for topologies that expose the API directly. |

## Closed networks and air-gapped installs

On-prem GitLab CE often lives somewhere that cannot reach `ghcr.io`.

**Mirror into your own registry.** Every image reference is prefixed with
`FLOWLENS_REGISTRY`, so switching is one line in `.env`:

```bash
# On a machine with internet access
for image in flowlens-api flowlens-web; do
  docker pull ghcr.io/motoki1996/$image:v0.1.2
  docker tag ghcr.io/motoki1996/$image:v0.1.2 registry.example.local/flowlens/$image:v0.1.2
  docker push registry.example.local/flowlens/$image:v0.1.2
done

# In .env on the target
FLOWLENS_REGISTRY=registry.example.local/flowlens
```

**Or install from the offline bundle.** Every release has a
`flowlens-<version>-images-amd64.tar.gz` attached, containing the API, web
and Postgres images:

```bash
docker load < flowlens-v0.1.2-images-amd64.tar.gz
docker compose up -d
```

Upgrading air-gapped is the same procedure as
[Upgrading](#upgrading), with `docker load` of the new bundle in place of
`docker compose pull`.

## Configuration reference

Everything is read from the environment; `.env.example` is the annotated
full list. The ones that matter for self-hosting:

| Variable | Default | What it does |
| --- | --- | --- |
| `APP_PUBLIC_URL` | `http://localhost:4000` | Where this instance is reachable, from a browser and from GitLab. |
| `POSTGRES_PASSWORD` | — | Required. Bundled database's password. |
| `ENCRYPTION_KEY` | — | Required. Base64 32 bytes; encrypts GitLab tokens and webhook secrets at rest. |
| `FLOWLENS_VERSION` | `latest` | Release to run. Pin it. |
| `FLOWLENS_REGISTRY` | `ghcr.io/motoki1996` | Where images are pulled from. |
| `FLOWLENS_BIND` / `FLOWLENS_PORT` | `0.0.0.0` / `4000` | Where the web app is published. |
| `ALLOW_SIGNUP` | `true` | Whether new accounts can be registered. |
| `TRUSTED_PROXY_HOPS` | `1` in compose | Proxies trusted to have appended to `X-Forwarded-For`. |
| `RUN_MIGRATIONS` | `true` | Apply embedded migrations at startup. |
| `METRICS_TOKEN` | empty | Bearer token required by `/metrics` when set. |
| `SESSION_TTL_HOURS` | `168` | Session lifetime. |
| `SYNC_WORKER_ENABLED` | `true` | Whether the background GitLab sync workers run. |
| `GITLAB_CA_CERT_FILE` | empty | PEM bundle trusted when calling GitLab; setting it turns verification on. |
| `GITLAB_TLS_INSECURE_SKIP_VERIFY` | `true` | Skip GitLab certificate verification. |

## Troubleshooting

**`ENCRYPTION_KEY is required` and the API will not start.** The key is
missing or not 32 bytes of base64. Regenerate:
`docker run --rm ghcr.io/motoki1996/flowlens-api:latest gen-key >> .env`.

**Login appears to succeed but every page bounces back to the login
screen.** The session cookie is being dropped. Almost always this is
`APP_ENV=production` (cookie marked `Secure`) served over plain HTTP — see
[Putting it on a real hostname](#putting-it-on-a-real-hostname).

**"Too many requests" on login for everyone at once.**
`TRUSTED_PROXY_HOPS` is lower than your real proxy chain, so every request
looks like it comes from the proxy and one shared limit covers all users.

**`schema is marked dirty at version N`.** A migration failed part-way,
probably because the container was killed mid-upgrade. Restore the backup
you took before upgrading. If you have none, `docker compose exec db psql -U
flowlens flowlens -c 'select * from schema_migrations'` shows the recorded
version; resolving it by hand means undoing the partial migration and
clearing the dirty flag, and is worth opening an issue about.

**GitLab webhooks never arrive.** `APP_PUBLIC_URL` must be reachable *from
the GitLab server*, not just from your browser. Check the delivery log on
the GitLab project's webhook settings page.

**`exec format error` on startup.** An image for the wrong architecture.
Releases are multi-arch (amd64 and arm64); the offline bundle is amd64 only.

**The web container cannot reach the API after I renamed the service.** The
proxy destination is fixed at image build time — Next.js resolves rewrites
during `next build`, not per request — and the published image is built for
`http://api:8080`. Keep the API service named `api`, or build your own web
image with `API_INTERNAL_URL` set:
`docker build --build-arg ... -t my/flowlens-web apps/web` after setting
`API_INTERNAL_URL` in the build environment. Setting it on a running
container has no effect on the proxy.
