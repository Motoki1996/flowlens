// Package migrations embeds the golang-migrate SQL files into the API
// binary so a self-hosted deployment never needs the `migrate` CLI: the
// server applies its own schema on startup (see internal/database.Migrate
// and docs/self-hosting.md).
//
// The .sql files stay the single source of truth — sqlc reads the *.up.sql
// files as its schema (sqlc.yaml) and the `migrate` CLI still works against
// this directory for local development.
package migrations

import "embed"

// FS holds every migration file in this directory.
//
//go:embed *.sql
var FS embed.FS
