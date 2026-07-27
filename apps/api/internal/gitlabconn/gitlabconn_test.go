package gitlabconn_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New([]byte("01234567890123456789012345678901"[:32]))
	require.NoError(t, err)
	return c
}

// newService builds a Service backed by an in-memory querier, whose
// verification calls all go to fake instead of a real GitLab CE instance.
func newService(t *testing.T, q *dbtest.FakeQuerier, fake *gitlab.FakeClient) *gitlabconn.Service {
	t.Helper()
	return gitlabconn.NewService(q, project.NewService(q), testCipher(t), func(string) gitlab.Client { return fake })
}

const validToken = "glpat-abcdefghijklmnopqrst"

func TestService_Save_VerifiesBeforeStoring(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 42, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	conn, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com/", validToken)
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.example.com", conn.BaseURL, "trailing slash must be normalized away")
	assert.Equal(t, "qrst", conn.TokenLastFour)
	assert.Equal(t, int64(42), *conn.TokenGitlabUserID)
	assert.Equal(t, "octocat", conn.TokenGitlabUsername)
	assert.True(t, conn.Verified)
	assert.Equal(t, 1, fake.Calls, "expected exactly one verification call")
}

func TestService_Save_NormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
		wantErr error
	}{
		{"trims trailing slash", "https://gitlab.example.com/", "https://gitlab.example.com", nil},
		{"trims trailing slashes and path slash", "https://gitlab.example.com/gitlab/", "https://gitlab.example.com/gitlab", nil},
		{"accepts http", "http://gitlab.internal", "http://gitlab.internal", nil},
		{"rejects missing scheme", "gitlab.example.com", "", gitlabconn.ErrInvalidBaseURL},
		{"rejects non-http(s) scheme", "ftp://gitlab.example.com", "", gitlabconn.ErrInvalidBaseURL},
		{"rejects empty", "", "", gitlabconn.ErrInvalidBaseURL},
		{"rejects whitespace only", "   ", "", gitlabconn.ErrInvalidBaseURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
			svc := newService(t, q, fake)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			conn, err := svc.Save(context.Background(), owner, p.ID, tt.baseURL, validToken)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, conn.BaseURL)
		})
	}
}

func TestService_Save_RejectsEmptyToken(t *testing.T) {
	q := dbtest.New()
	svc := newService(t, q, &gitlab.FakeClient{})
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", "   ")
	assert.ErrorIs(t, err, gitlabconn.ErrTokenRequired)
}

func TestService_Save_ReturnsNotFoundForForeignOrMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(t, q, &gitlab.FakeClient{})
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Save(context.Background(), other, p.ID, "https://gitlab.example.com", validToken)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound)

	_, err = svc.Save(context.Background(), owner, uuid.New(), "https://gitlab.example.com", validToken)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound)
}

// A verification failure must be classified distinctly (unreachable vs.
// invalid token vs. insufficient scope) and must never write a connection.
func TestService_Save_ClassifiesVerificationFailureAndWritesNothing(t *testing.T) {
	tests := []struct {
		name    string
		verErr  error
		wantErr error
	}{
		{"network error is unreachable", fmt.Errorf("dial tcp: connection refused"), gitlabconn.ErrUnreachable},
		{"5xx is unreachable", &gitlab.APIError{StatusCode: 503, Body: "unavailable"}, gitlabconn.ErrUnreachable},
		{"401 is invalid token", &gitlab.APIError{StatusCode: 401, Body: "unauthorized"}, gitlabconn.ErrTokenInvalid},
		{"403 is insufficient scope", &gitlab.APIError{StatusCode: 403, Body: "forbidden"}, gitlabconn.ErrInsufficientScope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			fake := &gitlab.FakeClient{Err: tt.verErr}
			svc := newService(t, q, fake)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
			assert.ErrorIs(t, err, tt.wantErr)

			_, getErr := svc.Get(context.Background(), owner, p.ID)
			assert.ErrorIs(t, getErr, gitlabconn.ErrNotFound, "a failed verification must not create a connection")
		})
	}
}

func TestService_Get_NeverExposesTheFullToken(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	conn, err := svc.Get(context.Background(), owner, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "qrst", conn.TokenLastFour)

	rendered := fmt.Sprintf("%+v", conn)
	assert.NotContains(t, rendered, validToken)
}

func TestService_Get_ReturnsNotFoundForForeignOrMissingConnection(t *testing.T) {
	q := dbtest.New()
	svc := newService(t, q, &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}})
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Get(context.Background(), owner, p.ID)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound, "no connection saved yet")

	_, err = svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	_, err = svc.Get(context.Background(), other, p.ID)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound)
}

func TestService_Save_ReplacesExistingConnection(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", "glpat-firsttoken1111")
	require.NoError(t, err)

	fake.AuthenticatedUser = &gitlab.User{ID: 2, Username: "hubot"}
	updated, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.internal", "glpat-secondtoken222")
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.internal", updated.BaseURL)
	assert.Equal(t, "hubot", updated.TokenGitlabUsername)
	assert.Equal(t, "n222", updated.TokenLastFour)
}

func TestService_Test_ReVerifiesAndPersistsOutcome(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	t.Run("success refreshes verification", func(t *testing.T) {
		fake.AuthenticatedUser = &gitlab.User{ID: 1, Username: "octocat-renamed"}
		conn, err := svc.Test(context.Background(), owner, p.ID)
		require.NoError(t, err)
		assert.True(t, conn.Verified)
		assert.Equal(t, "octocat-renamed", conn.TokenGitlabUsername)
	})

	t.Run("failure is classified and persisted, but the connection is kept", func(t *testing.T) {
		fake.AuthenticatedUser = nil
		fake.Err = &gitlab.APIError{StatusCode: 401, Body: "unauthorized"}

		_, err := svc.Test(context.Background(), owner, p.ID)
		assert.ErrorIs(t, err, gitlabconn.ErrTokenInvalid)

		conn, getErr := svc.Get(context.Background(), owner, p.ID)
		require.NoError(t, getErr, "the connection itself must survive a failed re-verification")
		assert.False(t, conn.Verified)
		assert.NotEmpty(t, conn.LastVerifyError)
	})
}

func TestService_Test_ReturnsNotFoundForForeignOrMissingConnection(t *testing.T) {
	q := dbtest.New()
	svc := newService(t, q, &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}})
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Test(context.Background(), owner, p.ID)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound)

	_, err = svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	_, err = svc.Test(context.Background(), other, p.ID)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound)
}

func TestService_Delete_RemovesConnection(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	require.NoError(t, svc.Delete(context.Background(), owner, p.ID))

	_, err = svc.Get(context.Background(), owner, p.ID)
	assert.ErrorIs(t, err, gitlabconn.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForForeignOrMissingConnection(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	assert.ErrorIs(t, svc.Delete(context.Background(), owner, p.ID), gitlabconn.ErrNotFound)

	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	assert.ErrorIs(t, svc.Delete(context.Background(), other, p.ID), gitlabconn.ErrNotFound)
}

// The token must be stored encrypted, never as the plaintext bytes, even in
// the fake querier's in-memory row — mirroring what the real BYTEA column
// holds (see the "not plaintext" integration test in internal/database).
func TestService_Save_StoresTokenEncrypted(t *testing.T) {
	q := dbtest.New()
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	svc := newService(t, q, fake)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Save(context.Background(), owner, p.ID, "https://gitlab.example.com", validToken)
	require.NoError(t, err)

	row, err := q.GetGitlabConnectionForOwner(context.Background(), db.GetGitlabConnectionForOwnerParams{
		ProjectID:   p.ID,
		OwnerUserID: owner,
	})
	require.NoError(t, err)
	assert.NotContains(t, string(row.EncryptedToken), validToken)
	assert.NotEqual(t, []byte(validToken), row.EncryptedToken)
}
