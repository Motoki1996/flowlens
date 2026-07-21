package user_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/github"
	"github.com/flowlens/api/internal/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCipher(t *testing.T) auth.TokenCipher {
	t.Helper()
	key := make([]byte, 32)
	c, err := auth.NewAESGCMCipher(key)
	require.NoError(t, err)
	return c
}

func TestService_UpsertFromGitHub_StoresEncryptedToken(t *testing.T) {
	q := dbtest.New()
	cipher := testCipher(t)
	svc := user.NewService(q, cipher)

	ghUser := &github.User{ID: 7, Login: "octocat", Name: "The Octocat", AvatarURL: "https://x/y.png"}
	row, err := svc.UpsertFromGitHub(context.Background(), ghUser, "gho_token")
	require.NoError(t, err)

	assert.Equal(t, int64(7), row.GithubUserID)
	assert.Equal(t, "octocat", row.GithubLogin)
	// The stored token must be encrypted, not plaintext.
	assert.NotEqual(t, "gho_token", string(row.EncryptedAccessToken))

	decrypted, err := cipher.Decrypt(row.EncryptedAccessToken)
	require.NoError(t, err)
	assert.Equal(t, "gho_token", decrypted)
}

func TestService_UpsertFromGitHub_IsIdempotent(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q, testCipher(t))
	ghUser := &github.User{ID: 7, Login: "octocat"}

	first, err := svc.UpsertFromGitHub(context.Background(), ghUser, "t1")
	require.NoError(t, err)

	ghUser.Login = "octocat-renamed"
	second, err := svc.UpsertFromGitHub(context.Background(), ghUser, "t2")
	require.NoError(t, err)

	// Same primary key, updated fields.
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "octocat-renamed", second.GithubLogin)
}

func TestFromDB_DoesNotExposeToken(t *testing.T) {
	q := dbtest.New()
	svc := user.NewService(q, testCipher(t))
	row, err := svc.UpsertFromGitHub(context.Background(), &github.User{ID: 1, Login: "a"}, "secret")
	require.NoError(t, err)

	dto := user.FromDB(row)
	assert.Equal(t, int64(1), dto.GitHubUserID)
	assert.Equal(t, "a", dto.GitHubLogin)
	assert.NotEmpty(t, dto.ID)
}
