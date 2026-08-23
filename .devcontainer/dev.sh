#!/usr/bin/env bash
# Starts the API and Web dev servers directly inside the devcontainer.
# Docker-in-Docker isn't available here, so this bypasses `docker compose`
# (used by `make dev` on the host) and runs both processes natively.
# The "db" service is already running as a sibling devcontainer service.
set -euo pipefail

cd "$(dirname "$0")/.."

# .env holds the host-oriented defaults (e.g. Postgres on localhost:55432).
# Values already exported into this container by compose describe the compose
# network instead (db:5432), so they must survive sourcing .env.
declare -A preset=()
for var in APP_ENV DATABASE_URL API_INTERNAL_URL NEXT_PUBLIC_API_BASE_URL; do
  if [ -n "${!var:-}" ]; then
    preset["$var"]="${!var}"
  fi
done

set -a
source .env
set +a

for var in "${!preset[@]}"; do
  export "$var=${preset[$var]}"
done

WEB_PORT="${WEB_PORT:-4000}"

# Next.js dev compiles a route the first time it is requested, so the first
# screen opened after every restart pays for it — the heaviest one here,
# /projects/[projectId], is ~3s of that. Requesting the routes ourselves as
# soon as the server is up moves that cost into the seconds the developer
# spends switching to the browser. Set FLOWLENS_DEV_WARM=0 to skip it.
#
# The cookie is a dummy: middleware.ts only checks that a session cookie is
# *present*, so this gets past the redirect to /login and makes Next compile
# the route. getCurrentUser() then rejects the bogus token and the request
# redirects — which is fine, compiling the route is the whole point. The
# placeholder project id is never looked up for the same reason.
warm_routes() {
  local base="http://localhost:$WEB_PORT"
  local placeholder="00000000-0000-0000-0000-000000000000"

  # Wait for the dev server to accept connections before asking for anything.
  for _ in $(seq 1 300); do
    curl -s -o /dev/null "$base/login" && break
    sleep 0.5
  done

  for route in \
    /dashboard \
    /tasks \
    /projects \
    /settings \
    "/projects/$placeholder" \
    "/projects/$placeholder/tasks" \
    "/projects/$placeholder/backlogs" \
    "/projects/$placeholder/merge-requests"
  do
    curl -s -o /dev/null -H 'Cookie: flowlens_session=warmup' "$base$route" || true
  done
}

pids=()
warm_pid=""
cleanup() {
  trap - INT TERM EXIT
  kill "${pids[@]}" ${warm_pid:+"$warm_pid"} 2>/dev/null || true
  wait "${pids[@]}" ${warm_pid:+"$warm_pid"} 2>/dev/null || true
}
trap cleanup INT TERM EXIT

(cd apps/api && exec air) &
pids+=("$!")

(cd apps/web && exec npm run dev) &
pids+=("$!")

# Deliberately not in `pids`: the warmer exits after a few seconds by design,
# and `wait -n` below returns on the *first* child to exit — counting it would
# tear the whole stack down as soon as the warm-up finished.
if [ "${FLOWLENS_DEV_WARM:-1}" != "0" ]; then
  warm_routes &
  warm_pid=$!
fi

wait -n "${pids[@]}"
