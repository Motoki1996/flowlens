#!/usr/bin/env bash
# Runs once after the devcontainer is created: install deps and prepare the DB.
set -euo pipefail

cd "$(dirname "$0")/.."

# DB host inside the compose network (differs from the host's .env value).
DB_URL="postgres://flowlens:flowlens@db:5432/flowlens?sslmode=disable"

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
#    The DATABASE_URL override on the command line beats the .env value, so
#    this works even though .env points at the host port (localhost:55432).
echo "==> waiting for postgres"
until pg_isready -h db -U flowlens -d flowlens >/dev/null 2>&1; do
  sleep 1
done
echo "==> applying migrations"
make migrate DATABASE_URL="$DB_URL"

echo "==> devcontainer ready"
