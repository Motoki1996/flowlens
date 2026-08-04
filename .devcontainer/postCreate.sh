#!/usr/bin/env bash
# Runs once after the devcontainer is created: install deps and prepare the DB.
set -euo pipefail

cd "$(dirname "$0")/.."

# 1. Ensure a .env exists (app runtime + Makefile read it on a fresh clone).
if [ ! -f .env ]; then
  cp .env.example .env
  echo "Created .env from .env.example"
fi

# 2. Install dependencies.
echo "==> go mod download"
( cd apps/api && go mod download )
echo "==> npm install"
( cd apps/web && npm install )

# 3. Wait for Postgres, then apply migrations.
#    DATABASE_URL comes from the compose environment (db:5432) and the Makefile
#    keeps it in preference to the .env value (the host port, localhost:55432).
echo "==> waiting for postgres"
until pg_isready -h db -U flowlens -d flowlens >/dev/null 2>&1; do
  sleep 1
done
echo "==> applying migrations"
make migrate

echo "==> devcontainer ready"
