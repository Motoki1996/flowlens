//go:build integration

// Integration tests exercise the real generated queries against a live
// PostgreSQL instance. Run with: make test-integration
// (requires DATABASE_URL and migrations already applied).
package database_test

import (
	"context"
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

func TestUpsertUserAndSessionRoundTrip(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	// Unique GitHub ID per run to keep tests independent.
	githubID := time.Now().UnixNano()

	u, err := q.UpsertUser(ctx, db.UpsertUserParams{
		GithubUserID:         githubID,
		GithubLogin:          "integration-user",
		DisplayName:          "Integration User",
		EncryptedAccessToken: []byte("enc-token"),
	})
	require.NoError(t, err)
	assert.Equal(t, githubID, u.GithubUserID)

	// Upsert again with a changed login -> same row updated.
	u2, err := q.UpsertUser(ctx, db.UpsertUserParams{
		GithubUserID:         githubID,
		GithubLogin:          "renamed",
		EncryptedAccessToken: []byte("enc-token-2"),
	})
	require.NoError(t, err)
	assert.Equal(t, u.ID, u2.ID)
	assert.Equal(t, "renamed", u2.GithubLogin)

	// Create a session and resolve the user through the join query.
	_, err = q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    u.ID,
		TokenHash: "hash-" + u.GithubLogin,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)

	row, err := q.GetUserBySessionToken(ctx, "hash-"+u.GithubLogin)
	require.NoError(t, err)
	assert.Equal(t, githubID, row.User.GithubUserID)

	// Cleanup.
	require.NoError(t, q.DeleteSessionByTokenHash(ctx, "hash-"+u.GithubLogin))
}
