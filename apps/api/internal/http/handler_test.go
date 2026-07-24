package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer builds a Server wired to an in-memory querier. The pool is
// nil because these tests never exercise the health handler.
func newTestServer(t *testing.T) (*Server, *dbtest.FakeQuerier) {
	t.Helper()
	q := dbtest.New()
	return &Server{
		users:      user.NewService(q),
		sessions:   auth.NewSessionService(q, time.Hour),
		cookies:    cookieManager{secure: false},
		webBaseURL: "http://localhost:3000",
		sessionTTL: time.Hour,
	}, q
}

func loginSession(t *testing.T, s *Server, q *dbtest.FakeQuerier) (db.User, string) {
	t.Helper()
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Username:     "tester",
		Email:        "tester@example.com",
		DisplayName:  "Test User",
		PasswordHash: "irrelevant-for-session-tests",
	})
	require.NoError(t, err)
	token, err := s.sessions.Create(context.Background(), u.ID)
	require.NoError(t, err)
	return u, token
}

func postJSON(t *testing.T, s *Server, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
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
	assert.Equal(t, "tester", body["username"])
	assert.Equal(t, "Test User", body["displayName"])
	// The password hash must never appear in the response.
	assert.NotContains(t, rec.Body.String(), "irrelevant-for-session-tests")
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

func TestHandleSignup_CreatesUserAndSession(t *testing.T) {
	s, _ := newTestServer(t)

	rec := postJSON(t, s, "/auth/signup", signupRequest{
		Username: "octocat", Email: "octocat@example.com", Password: "hunter22",
	})

	require.Equal(t, http.StatusCreated, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "octocat", body["username"])

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie, "expected a session cookie to be set")
	assert.NotEmpty(t, sessionCookie.Value)
}

func TestHandleSignup_DuplicateUsername(t *testing.T) {
	s, _ := newTestServer(t)
	rec := postJSON(t, s, "/auth/signup", signupRequest{Username: "octocat", Email: "a@example.com", Password: "hunter22"})
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = postJSON(t, s, "/auth/signup", signupRequest{Username: "octocat", Email: "b@example.com", Password: "hunter22"})
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleSignup_PasswordTooShort(t *testing.T) {
	s, _ := newTestServer(t)
	rec := postJSON(t, s, "/auth/signup", signupRequest{Username: "octocat", Email: "a@example.com", Password: "short"})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleLogin_Succeeds(t *testing.T) {
	s, _ := newTestServer(t)
	rec := postJSON(t, s, "/auth/signup", signupRequest{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"})
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = postJSON(t, s, "/auth/login", loginRequest{Identifier: "octocat", Password: "hunter22"})
	require.Equal(t, http.StatusOK, rec.Code)

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie)
}

func TestHandleLogin_WrongPassword(t *testing.T) {
	s, _ := newTestServer(t)
	rec := postJSON(t, s, "/auth/signup", signupRequest{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"})
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = postJSON(t, s, "/auth/login", loginRequest{Identifier: "octocat", Password: "wrong-password"})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
