package user_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/user"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_SignUp_HashesPassword(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	u, err := svc.SignUp(ctx, user.SignUpInput{
		Username: "octocat", Email: "octocat@example.com", Password: "hunter22",
	})
	require.NoError(t, err)

	assert.Equal(t, "octocat", u.Username)
	assert.Equal(t, "octocat@example.com", u.Email)
	assert.Equal(t, "octocat", u.DisplayName) // defaults to username

	// The stored row must hold a hash, not the plaintext password. Reading
	// it back through the querier is the only way to see it: the domain
	// User type has no field for it.
	stored, err := q.GetUserByUsernameOrEmail(ctx, "octocat")
	require.NoError(t, err)
	assert.NotEqual(t, "hunter22", stored.PasswordHash)
	assert.NotEmpty(t, stored.PasswordHash)
}

func TestService_SignUp_RejectsShortPassword(t *testing.T) {
	svc := user.NewService(dbtest.New())

	_, err := svc.SignUp(context.Background(), user.SignUpInput{
		Username: "octocat", Email: "octocat@example.com", Password: "short",
	})
	assert.ErrorIs(t, err, user.ErrPasswordTooShort)
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

func TestService_ByID(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	created, err := svc.SignUp(ctx, user.SignUpInput{Username: "a", Email: "a@example.com", Password: "hunter22"})
	require.NoError(t, err)

	got, err := svc.ByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	_, err = svc.ByID(ctx, uuid.New())
	assert.ErrorIs(t, err, user.ErrNotFound)
}

// The JSON sent to clients must never carry a password hash, and the ID
// must be the canonical UUID form.
func TestUser_JSONShape(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	u, err := svc.SignUp(context.Background(), user.SignUpInput{Username: "a", Email: "a@example.com", Password: "hunter22"})
	require.NoError(t, err)

	encoded, err := json.Marshal(u)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, []string{"displayName", "email", "id", "username"}, sortedKeys(decoded))
	assert.Equal(t, u.ID.String(), decoded["id"])
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestService_ChangePassword_ReplacesTheHash(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q)
	ctx := context.Background()

	u, err := svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"})
	require.NoError(t, err)
	before, err := q.GetUserByID(ctx, u.ID)
	require.NoError(t, err)

	require.NoError(t, svc.ChangePassword(ctx, u.ID, "hunter22", "correct-horse"))

	// The new password authenticates and the old one no longer does — the
	// property that matters, checked through the service rather than by
	// comparing hashes.
	_, err = svc.Authenticate(ctx, "octocat", "correct-horse")
	assert.NoError(t, err)
	_, err = svc.Authenticate(ctx, "octocat", "hunter22")
	assert.ErrorIs(t, err, user.ErrInvalidCredentials)

	after, err := q.GetUserByID(ctx, u.ID)
	require.NoError(t, err)
	assert.NotEqual(t, before.PasswordHash, after.PasswordHash)
	assert.NotContains(t, after.PasswordHash, "correct-horse")
}

func TestService_ChangePassword_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		current string
		next    string
		wantErr error
	}{
		{"wrong current password", "not-my-password", "correct-horse", user.ErrInvalidCredentials},
		{"empty current password", "", "correct-horse", user.ErrInvalidCredentials},
		{"new password too short", "hunter22", "short", user.ErrPasswordTooShort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := user.NewService(q)
			ctx := context.Background()
			u, err := svc.SignUp(ctx, user.SignUpInput{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"})
			require.NoError(t, err)

			err = svc.ChangePassword(ctx, u.ID, tt.current, tt.next)
			assert.ErrorIs(t, err, tt.wantErr)

			// A rejected change must leave the old password working.
			_, err = svc.Authenticate(ctx, "octocat", "hunter22")
			assert.NoError(t, err)
		})
	}
}

func TestService_ChangePassword_UnknownUser(t *testing.T) {
	svc := user.NewService(dbtest.New())

	err := svc.ChangePassword(context.Background(), uuid.New(), "hunter22", "correct-horse")
	assert.ErrorIs(t, err, user.ErrNotFound)
}
