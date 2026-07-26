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
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/user"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer builds a Server wired to an in-memory querier. health is
// nil because these tests never exercise the health handler.
func newTestServer(t *testing.T) (*Server, *dbtest.FakeQuerier) {
	t.Helper()
	q := dbtest.New()
	return &Server{
		users:      user.NewService(q),
		projects:   project.NewService(q),
		sessions:   auth.NewSessionService(q, time.Hour),
		cookies:    cookieManager{secure: false},
		webBaseURL: "http://localhost:3000",
		sessionTTL: time.Hour,
	}, q
}

// loginSession seeds a user and issues a session for it, returning the
// user's ID and the raw session token.
func loginSession(t *testing.T, s *Server, q *dbtest.FakeQuerier) (uuid.UUID, string) {
	t.Helper()
	u := q.SeedUser("tester", "tester@example.com")
	token, err := s.sessions.Create(context.Background(), u.ID)
	require.NoError(t, err)
	return u.ID, token
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

// sessionCookie returns the session cookie set on the response, or nil.
func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
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
	assert.Equal(t, "tester", body["displayName"])
	// The password hash must never appear in the response.
	assert.NotContains(t, rec.Body.String(), "seeded-hash")
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

func TestHandleSignup(t *testing.T) {
	valid := signupRequest{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"}
	tests := []struct {
		name       string
		seed       func(q *dbtest.FakeQuerier) // optional pre-existing state
		req        signupRequest
		wantCode   int
		wantCookie bool
	}{
		{"creates user and session", nil, valid, http.StatusCreated, true},
		{
			name:     "duplicate username",
			seed:     func(q *dbtest.FakeQuerier) { q.SeedUser("octocat", "taken@example.com") },
			req:      valid,
			wantCode: http.StatusConflict,
		},
		{
			name:     "password too short",
			req:      signupRequest{Username: "octocat", Email: "octocat@example.com", Password: "short"},
			wantCode: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			if tt.seed != nil {
				tt.seed(q)
			}
			rec := postJSON(t, s, "/auth/signup", tt.req)

			require.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCookie {
				c := sessionCookie(rec)
				require.NotNil(t, c, "expected a session cookie to be set")
				assert.NotEmpty(t, c.Value)
			}
		})
	}
}

func TestHandleLogin(t *testing.T) {
	const password = "hunter22"
	tests := []struct {
		name       string
		password   string
		wantCode   int
		wantCookie bool
	}{
		{"succeeds", password, http.StatusOK, true},
		{"wrong password", "wrong-password", http.StatusUnauthorized, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			require.Equal(t, http.StatusCreated,
				postJSON(t, s, "/auth/signup", signupRequest{Username: "octocat", Email: "octocat@example.com", Password: password}).Code)

			rec := postJSON(t, s, "/auth/login", loginRequest{Identifier: "octocat", Password: tt.password})

			require.Equal(t, tt.wantCode, rec.Code)
			assert.Equal(t, tt.wantCookie, sessionCookie(rec) != nil)
		})
	}
}
