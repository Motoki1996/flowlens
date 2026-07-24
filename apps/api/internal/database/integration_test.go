//go:build integration

// Integration tests exercise the real generated queries against a live
// PostgreSQL instance. Run with: make test-integration
// (requires DATABASE_URL and migrations already applied).
package database_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *db.Queries {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := database.Connect(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return db.New(pool)
}

func TestCreateUserAndSessionRoundTrip(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	// Unique username/email per run to keep tests independent.
	suffix := time.Now().UnixNano()
	username := fmt.Sprintf("integration-user-%d", suffix)
	email := fmt.Sprintf("integration-%d@example.com", suffix)

	u, err := q.CreateUser(ctx, db.CreateUserParams{
		Username:     username,
		Email:        email,
		DisplayName:  "Integration User",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	assert.Equal(t, username, u.Username)

	got, err := q.GetUserByUsernameOrEmail(ctx, email)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)

	// Create a session and resolve the user through the join query.
	_, err = q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    u.ID,
		TokenHash: "hash-" + username,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)

	row, err := q.GetUserBySessionToken(ctx, "hash-"+username)
	require.NoError(t, err)
	assert.Equal(t, username, row.User.Username)

	// Cleanup.
	require.NoError(t, q.DeleteSessionByTokenHash(ctx, "hash-"+username))
}
