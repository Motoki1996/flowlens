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
	"github.com/jackc/pgx/v5"
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

// createUser inserts a user with a name unique to this run, so tests stay
// independent of whatever previous runs left behind.
func createUser(t *testing.T, q *db.Queries, label string) db.User {
	t.Helper()
	suffix := fmt.Sprintf("%s-%d", label, time.Now().UnixNano())
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Username:     "integration-" + suffix,
		Email:        fmt.Sprintf("integration-%s@example.com", suffix),
		DisplayName:  "Integration User",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	return u
}

// Ownership is enforced by the WHERE clause of every project query, not by
// the caller. This exercises the real SQL: the fake querier can only mirror
// what the database actually does.
func TestProjectQueriesEnforceOwnership(t *testing.T) {
	q := testDB(t)
	ctx := context.Background()

	owner := createUser(t, q, "owner")
	intruder := createUser(t, q, "intruder")

	p, err := q.CreateProject(ctx, db.CreateProjectParams{
		OwnerUserID: owner.ID,
		Name:        "Alpha",
		Description: "desc",
	})
	require.NoError(t, err)

	t.Run("owner reads its own project", func(t *testing.T) {
		got, err := q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, p.ID, got.ID)
	})

	t.Run("non-owner gets no rows", func(t *testing.T) {
		_, err := q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{ID: p.ID, OwnerUserID: intruder.ID})
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("non-owner update writes nothing", func(t *testing.T) {
		_, err := q.UpdateProjectForOwner(ctx, db.UpdateProjectForOwnerParams{
			ID: p.ID, OwnerUserID: intruder.ID, Name: "Hijacked",
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)

		still, err := q.GetProjectForOwner(ctx, db.GetProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, "Alpha", still.Name)
	})

	t.Run("non-owner delete affects no rows", func(t *testing.T) {
		affected, err := q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: intruder.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})

	t.Run("owner delete affects one row, and a repeat affects none", func(t *testing.T) {
		affected, err := q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		require.Equal(t, int64(1), affected)

		affected, err = q.DeleteProjectForOwner(ctx, db.DeleteProjectForOwnerParams{ID: p.ID, OwnerUserID: owner.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(0), affected)
	})
}
