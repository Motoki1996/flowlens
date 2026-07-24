package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedUser(t *testing.T, q *dbtest.FakeQuerier) db.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Username:     "octocat",
		Email:        "octocat@example.com",
		PasswordHash: "hash",
	})
	require.NoError(t, err)
	return u
}

func TestSessionService_CreateAndAuthenticate(t *testing.T) {
	q := dbtest.New()
	u := seedUser(t, q)
	svc := auth.NewSessionService(q, time.Hour)

	token, err := svc.Create(context.Background(), u.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	got, err := svc.Authenticate(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, u.Username, got.Username)
}

func TestSessionService_AuthenticateUnknownToken(t *testing.T) {
	svc := auth.NewSessionService(dbtest.New(), time.Hour)
	_, err := svc.Authenticate(context.Background(), "nope")
	assert.ErrorIs(t, err, auth.ErrSessionNotFound)
}

func TestSessionService_AuthenticateEmptyToken(t *testing.T) {
	svc := auth.NewSessionService(dbtest.New(), time.Hour)
	_, err := svc.Authenticate(context.Background(), "")
	assert.ErrorIs(t, err, auth.ErrSessionNotFound)
}

func TestSessionService_Revoke(t *testing.T) {
	q := dbtest.New()
	u := seedUser(t, q)
	svc := auth.NewSessionService(q, time.Hour)

	token, err := svc.Create(context.Background(), u.ID)
	require.NoError(t, err)

	require.NoError(t, svc.Revoke(context.Background(), token))
	_, err = svc.Authenticate(context.Background(), token)
	assert.ErrorIs(t, err, auth.ErrSessionNotFound)
}

func TestSessionService_ExpiredSessionRejected(t *testing.T) {
	q := dbtest.New()
	u := seedUser(t, q)
	// TTL in the past -> session already expired.
	svc := auth.NewSessionService(q, -time.Hour)

	token, err := svc.Create(context.Background(), u.ID)
	require.NoError(t, err)

	_, err = svc.Authenticate(context.Background(), token)
	assert.ErrorIs(t, err, auth.ErrSessionNotFound)
}
