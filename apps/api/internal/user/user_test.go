package user_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_SignUp_HashesPassword(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)

	row, err := svc.SignUp(context.Background(), user.SignUpInput{
		Username: "octocat", Email: "octocat@example.com", Password: "hunter22",
	})
	require.NoError(t, err)

	assert.Equal(t, "octocat", row.Username)
	assert.Equal(t, "octocat@example.com", row.Email)
	assert.Equal(t, "octocat", row.DisplayName) // defaults to username
	assert.NotEqual(t, "hunter22", row.PasswordHash)
}

func TestService_SignUp_RejectsDuplicateUsername(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "a@example.com", Password: "hunter22"})
	require.NoError(t, err)

	_, err = svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "b@example.com", Password: "hunter22"})
	assert.ErrorIs(t, err, user.ErrUsernameTaken)
}

func TestService_SignUp_RejectsDuplicateEmail(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "a@example.com", Password: "hunter22"})
	require.NoError(t, err)

	_, err = svc.SignUp(ctx, user.SignUpInput{Username: "other", Email: "a@example.com", Password: "hunter22"})
	assert.ErrorIs(t, err, user.ErrEmailTaken)
}

func TestService_Authenticate_Succeeds(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	created, err := svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"})
	require.NoError(t, err)

	byUsername, err := svc.Authenticate(ctx, "octocat", "hunter22")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byUsername.ID)

	byEmail, err := svc.Authenticate(ctx, "octocat@example.com", "hunter22")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byEmail.ID)
}

func TestService_Authenticate_WrongPassword(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	_, err := svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"})
	require.NoError(t, err)

	_, err = svc.Authenticate(ctx, "octocat", "wrong-password")
	assert.ErrorIs(t, err, user.ErrInvalidCredentials)
}

func TestService_Authenticate_UnknownUser(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)

	_, err := svc.Authenticate(context.Background(), "nobody", "hunter22")
	assert.ErrorIs(t, err, user.ErrInvalidCredentials)
}

func TestFromDB_DoesNotExposePasswordHash(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	row, err := svc.SignUp(context.Background(), user.SignUpInput{Username: "a", Email: "a@example.com", Password: "hunter22"})
	require.NoError(t, err)

	dto := user.FromDB(row)
	assert.Equal(t, "a", dto.Username)
	assert.Equal(t, "a@example.com", dto.Email)
	assert.NotEmpty(t, dto.ID)
}
