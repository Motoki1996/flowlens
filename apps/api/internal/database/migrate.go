package database

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx5" database driver with golang-migrate. The project
	// already talks to Postgres through pgx v5, so the migrator uses the
	// same driver rather than pulling in a second Postgres stack.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migrate applies every pending up migration in fsys against databaseURL.
//
// It runs at API startup so a self-hosted deployment is a single container
// with no separate `migrate` step (docs/self-hosting.md). Applying an
// already-current schema is a no-op, so running it on every boot is safe,
// and golang-migrate holds a Postgres advisory lock for the duration —
// several API replicas starting at once serialise here rather than racing,
// at the cost of the waiting ones booting correspondingly later.
func Migrate(databaseURL string, fsys fs.FS) error {
	source, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("database: open embedded migrations: %w", err)
	}
	defer func() { _ = source.Close() }()

	m, err := migrate.NewWithSourceInstance("iofs", source, migrateURL(databaseURL))
	if err != nil {
		return fmt.Errorf("database: init migrator: %w", err)
	}
	// Only the source is closed here: the migrator's database handle is a
	// connection of its own, and closing it is what the deferred Close
	// below does. The pool the rest of the API uses is untouched.
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			slog.Warn("migrator did not close cleanly", "source_error", srcErr, "database_error", dbErr)
		}
	}()

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("database: read schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("database: schema is marked dirty at version %d — a previous migration failed part-way and must be resolved by hand before the API can start; see docs/self-hosting.md", version)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("database schema is up to date", "version", version)
			return nil
		}
		return fmt.Errorf("database: apply migrations: %w", err)
	}

	newVersion, _, err := m.Version()
	if err != nil {
		return fmt.Errorf("database: read schema version after migrating: %w", err)
	}
	slog.Info("database migrations applied", "from_version", version, "to_version", newVersion)
	return nil
}

// migrateURL rewrites the connection URL's scheme to the one golang-migrate
// registers for the pgx v5 driver. DATABASE_URL is written for the
// application (and for psql, and for the `migrate` CLI), so it carries the
// conventional postgres:// scheme; golang-migrate dispatches on the scheme
// to pick a driver, and would not find "pgx5" behind it.
func migrateURL(databaseURL string) string {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(databaseURL, scheme) {
			return "pgx5://" + strings.TrimPrefix(databaseURL, scheme)
		}
	}
	return databaseURL
}
