package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

// The embed pattern silently matching nothing would ship a binary that
// starts against an empty database and then fails on the first query, so
// assert the files are actually in there — and that every up migration has
// the down file the rollback path in docs/self-hosting.md assumes.
func TestEmbeddedMigrations(t *testing.T) {
	entries, err := fs.Glob(FS, "*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations embedded — check the //go:embed pattern")
	}

	ups := map[string]bool{}
	downs := map[string]bool{}
	for _, name := range entries {
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			ups[strings.TrimSuffix(name, ".up.sql")] = true
		case strings.HasSuffix(name, ".down.sql"):
			downs[strings.TrimSuffix(name, ".down.sql")] = true
		default:
			t.Errorf("%s is neither an up nor a down migration", name)
		}
	}

	for version := range ups {
		if !downs[version] {
			t.Errorf("%s has no .down.sql", version)
		}
	}
	for version := range downs {
		if !ups[version] {
			t.Errorf("%s has no .up.sql", version)
		}
	}
}
