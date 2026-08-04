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

pids=()
cleanup() {
  trap - INT TERM EXIT
  kill "${pids[@]}" 2>/dev/null || true
  wait "${pids[@]}" 2>/dev/null || true
}
trap cleanup INT TERM EXIT

(cd apps/api && exec air) &
pids+=("$!")

(cd apps/web && exec npm run dev) &
pids+=("$!")

wait -n "${pids[@]}"
