#!/usr/bin/env bash
# Starts the API and Web dev servers directly inside the devcontainer.
# Docker-in-Docker isn't available here, so this bypasses `docker compose`
# (used by `make dev` on the host) and runs both processes natively.
# The "db" service is already running as a sibling devcontainer service.
set -euo pipefail

cd "$(dirname "$0")/.."

set -a
source .env
set +a

# Inside the devcontainer, Postgres is reached via the compose service name,
# not the host port (localhost:55432) recorded in .env.
export DATABASE_URL="postgres://flowlens:flowlens@db:5432/flowlens?sslmode=disable"

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
