package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer builds a Server wired to an in-memory querier. The pool is
// nil because these tests never exercise the health handler.
func newTestServer(t *testing.T) (*Server, *dbtest.FakeQuerier) {
	t.Helper()
	q := dbtest.New()
	return &Server{
		sessions:   auth.NewSessionService(q, time.Hour),
		cookies:    cookieManager{secure: false},
		webBaseURL: "http://localhost:3000",
		sessionTTL: time.Hour,
	}, q
}

func loginSession(t *testing.T, s *Server, q *dbtest.FakeQuerier) (db.User, string) {
	t.Helper()
	u, err := q.UpsertUser(context.Background(), db.UpsertUserParams{
		GithubUserID:         99,
		GithubLogin:          "tester",
		DisplayName:          "Test User",
		EncryptedAccessToken: []byte("enc"),
	})
	require.NoError(t, err)
	token, err := s.sessions.Create(context.Background(), u.ID)
	require.NoError(t, err)
	return u, token
}

func TestHandleMe_Authenticated(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "tester", body["githubLogin"])
	assert.Equal(t, "Test User", body["displayName"])
	// The encrypted token must never appear in the response.
	assert.NotContains(t, rec.Body.String(), "encrypted")
}

func TestHandleMe_NoCookie(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleMe_InvalidSession(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bogus"})
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleLogout_ClearsCookieAndRevokes(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	// Session must be revoked afterwards.
	_, err := s.sessions.Authenticate(context.Background(), token)
	assert.ErrorIs(t, err, auth.ErrSessionNotFound)

	// A Set-Cookie clearing the session must be present.
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "expected session cookie to be cleared")
}

func TestHandleGitHubLogin_NotConfigured(t *testing.T) {
	s, _ := newTestServer(t)
	s.oauth = auth.NewGitHubOAuthConfig("", "", "http://localhost:8080")
	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandleGitHubLogin_RedirectsToGitHub(t *testing.T) {
	s, _ := newTestServer(t)
	s.oauth = auth.NewGitHubOAuthConfig("client-id", "secret", "http://localhost:8080")
	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "github.com/login/oauth/authorize")
	// A state cookie must be set.
	var hasState bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookieName && c.Value != "" {
			hasState = true
		}
	}
	assert.True(t, hasState)
}
