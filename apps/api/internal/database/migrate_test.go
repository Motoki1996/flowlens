package database

import "testing"

func TestMigrateURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "the conventional postgres:// scheme is swapped for the pgx driver's",
			in:   "postgres://flowlens:pw@db:5432/flowlens?sslmode=disable",
			want: "pgx5://flowlens:pw@db:5432/flowlens?sslmode=disable",
		},
		{
			name: "the postgresql:// spelling is handled too",
			in:   "postgresql://flowlens@db:5432/flowlens",
			want: "pgx5://flowlens@db:5432/flowlens",
		},
		{
			name: "an already-pgx5 URL is left alone",
			in:   "pgx5://flowlens@db:5432/flowlens",
			want: "pgx5://flowlens@db:5432/flowlens",
		},
		{
			name: "a password containing the scheme text is not rewritten twice",
			in:   "postgres://user:postgres%3A%2F%2F@db:5432/flowlens",
			want: "pgx5://user:postgres%3A%2F%2F@db:5432/flowlens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := migrateURL(tt.in); got != tt.want {
				t.Errorf("migrateURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
