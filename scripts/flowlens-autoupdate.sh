#!/usr/bin/env bash
#
# Check GitHub for a newer FlowLens release and, when that release is one
# that is safe to apply unattended, back the database up and roll the compose
# stack onto it. Meant to run from cron on the host running the stack — see
# the "Automatic updates" section of docs/self-hosting.md.
#
#   FLOWLENS_DIR=/srv/flowlens /usr/local/bin/flowlens-autoupdate.sh
#
# Needs nothing on the host beyond bash, curl, sed, grep and the Docker CLI:
# no jq, no python. That is deliberate — the machine running FlowLens is
# often a bare box whose only real dependency is Docker.
#
# Exit codes: 0 nothing to do, or upgraded and verified. 1 held — a human has
# to read the notes and upgrade by hand. 2 the upgrade itself failed.

set -Eeuo pipefail

# Directory holding compose.yaml and .env.
FLOWLENS_DIR="${FLOWLENS_DIR:-/srv/flowlens}"
BACKUP_DIR="${BACKUP_DIR:-$FLOWLENS_DIR/backups}"
BACKUP_KEEP="${BACKUP_KEEP:-14}"
# patch — apply only x.y.Z bumps. The default, and the only setting that is
#         unattended-safe while this project is pre-1.0: a minor here can
#         still change the API's shape (v0.3.0 paged the task collections).
# minor — apply x.Y.0 as well.
# Neither ever crosses a major version, and neither applies a release whose
# notes carry a Breaking marker.
AUTO_LEVEL="${AUTO_LEVEL:-patch}"
FLOWLENS_REPO="${FLOWLENS_REPO:-Motoki1996/flowlens}"
# Optional. Any endpoint accepting a JSON POST with a "text" key.
NOTIFY_WEBHOOK="${NOTIFY_WEBHOOK:-}"
# Match however you invoke compose, e.g. "-f compose.yaml -f compose.tls.yaml".
COMPOSE_FILES="${COMPOSE_FILES:--f compose.yaml}"

log() { printf '%s flowlens-autoupdate: %s\n' "$(date -Is)" "$*" >&2; }

notify() {
  [ -n "$NOTIFY_WEBHOOK" ] || return 0
  # Strip the two characters that would break the JSON literal. Every message
  # this script sends is prose and a URL, so nothing of value is lost.
  local text
  text="$(printf '%s' "$1" | tr -d '"\\')"
  curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
    --data "{\"text\":\"$text\"}" "$NOTIFY_WEBHOOK" >/dev/null \
    || log "could not post to NOTIFY_WEBHOOK (ignored)"
}

die()    { log "FAILED: $*"; notify "FlowLens auto-update FAILED: $*"; exit 2; }
hold()   { log "held: $*";   notify "FlowLens auto-update held: $*";   exit 1; }

cd "$FLOWLENS_DIR" || die "no such directory: $FLOWLENS_DIR"
[ -f .env ] || die "no .env in $FLOWLENS_DIR"
# shellcheck disable=SC2086  # COMPOSE_FILES is a list of arguments on purpose
compose() { docker compose $COMPOSE_FILES "$@"; }

# --- what is running now ------------------------------------------------
current="$(grep -E '^FLOWLENS_VERSION=' .env | tail -1 | cut -d= -f2- | tr -d "\"' ")"
[ -n "$current" ] || die "FLOWLENS_VERSION is not set in $FLOWLENS_DIR/.env"
[ "$current" != latest ] \
  || hold "FLOWLENS_VERSION is 'latest'. Pin it to a release tag before automating upgrades, or an unrelated pull moves you across a migration nobody read the notes for."

# --- what is available --------------------------------------------------
# Parsed with grep rather than a JSON tool to keep the host dependency-free.
release="$(curl -fsS -m 30 -H 'Accept: application/vnd.github+json' \
  "https://api.github.com/repos/$FLOWLENS_REPO/releases/latest")" \
  || die "could not reach the GitHub release API (a closed-network install cannot use this script — see docs/self-hosting.md)"

latest="$(printf '%s' "$release" \
  | grep -oE '"tag_name"[[:space:]]*:[[:space:]]*"[^"]+"' | head -1 \
  | sed 's/.*"\([^"]*\)"$/\1/')"
[ -n "$latest" ] || die "could not read tag_name from the release API response"

if [ "$latest" = "$current" ]; then
  log "up to date on $current"
  exit 0
fi

# --- is it safe to apply unattended? ------------------------------------
strip_v() { printf '%s' "${1#v}"; }
IFS=. read -r cmaj cmin _ <<<"$(strip_v "$current")"
IFS=. read -r lmaj lmin _ <<<"$(strip_v "$latest")"

case "$lmaj$lmin$cmaj$cmin" in
  *[!0-9]*|'') die "cannot compare $current with $latest as semver" ;;
esac

if [ "$lmaj" != "$cmaj" ]; then
  hold "$current -> $latest crosses a major version. Always a human's call."
fi
if [ "$lmaj.$lmin" != "$cmaj.$cmin" ] && [ "$AUTO_LEVEL" != minor ]; then
  hold "$current -> $latest is not a patch bump and AUTO_LEVEL=$AUTO_LEVEL. Read the notes and upgrade by hand."
fi
# An empty body means the notes were never written, so the Breaking marker
# cannot be trusted to be absent. Refuse rather than guess.
printf '%s' "$release" | grep -qE '"body"[[:space:]]*:[[:space:]]*"[^"]' \
  || hold "$latest has empty release notes, so this script cannot tell whether it is breaking."
# Matched against the whole document rather than the body alone; the marker
# appears nowhere else, and a false positive only ever holds the upgrade.
if printf '%s' "$release" | grep -qF '⚠️ Breaking'; then
  hold "$latest is marked Breaking. Read the notes and upgrade by hand: https://github.com/$FLOWLENS_REPO/releases/tag/$latest"
fi

log "upgrading $current -> $latest"

# --- back up ------------------------------------------------------------
# Not optional. A migration cannot be undone in place, and rolling the image
# back on its own leaves the newer schema under a binary that predates it.
mkdir -p "$BACKUP_DIR"
backup="$BACKUP_DIR/pre-$latest-$(date +%F-%H%M%S).sql.gz"
compose exec -T db pg_dump -U flowlens flowlens | gzip > "$backup" \
  || die "pg_dump failed. Nothing was changed."
[ -s "$backup" ] || die "the backup is empty. Nothing was changed."
cp .env "$BACKUP_DIR/env-pre-$latest.backup"
log "backed up to $backup"

# --- apply --------------------------------------------------------------
restore="gunzip -c $backup | docker compose exec -T db psql -U flowlens flowlens"
sed -i "s|^FLOWLENS_VERSION=.*|FLOWLENS_VERSION=$latest|" .env
compose pull \
  || die "pull failed. .env now names $latest but the running containers are still $current; re-run once the registry is reachable."
compose up -d \
  || die "up failed. Restore with: $restore"

# --- verify what actually came up ---------------------------------------
running=""
for _ in $(seq 30); do
  running="$(compose exec -T api /app/api version 2>/dev/null \
    | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
  if [ "$running" = "$latest" ]; then break; fi
  sleep 5
done
[ "$running" = "$latest" ] \
  || die "the API is not reporting $latest after 150s (it reports '${running:-nothing}'). Restore with: $restore"

if compose logs api --since 10m 2>/dev/null | grep -q 'database migrations applied'; then
  log "database migrations applied"
fi

# Keep the most recent BACKUP_KEEP dumps.
(ls -1t "$BACKUP_DIR"/pre-*.sql.gz 2>/dev/null || true) \
  | tail -n +"$((BACKUP_KEEP + 1))" | xargs -r rm -f --

log "upgraded to $latest"
notify "FlowLens upgraded $current -> $latest"
