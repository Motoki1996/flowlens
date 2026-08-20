package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/auth"
	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/deliverymetrics"
	"github.com/flowlens/api/internal/flowmetrics"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/gitlabconn"
	"github.com/flowlens/api/internal/gitlabidentity"
	"github.com/flowlens/api/internal/linkedproject"
	"github.com/flowlens/api/internal/mergerequest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectinvite"
	"github.com/flowlens/api/internal/projectmember"
	"github.com/flowlens/api/internal/projectsync"
	"github.com/flowlens/api/internal/syncjob"
	"github.com/flowlens/api/internal/task"
	"github.com/flowlens/api/internal/taskcomment"
	"github.com/flowlens/api/internal/taskdependency"
	"github.com/flowlens/api/internal/user"
	"github.com/flowlens/api/internal/velocity"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEncryptionKey is a fixed 32-byte AES-256 key for tests; it never
// protects real secrets.
var testEncryptionKey = []byte("01234567890123456789012345678901"[:32])

// newTestServer builds a Server wired to an in-memory querier. health is
// nil because these tests never exercise the health handler.
func newTestServer(t *testing.T) (*Server, *dbtest.FakeQuerier) {
	t.Helper()
	return newTestServerWithGitlabClient(t, &gitlab.FakeClient{})
}

// newTestServerWithGitlabClient is newTestServer, but every GitLab
// connection the server verifies goes through fake instead of a fresh,
// empty FakeClient — letting gitlab connection tests control what
// "verification" returns without a real GitLab CE instance. APP_PUBLIC_URL
// is unset, so linked GitLab projects never attempt webhook registration;
// use newTestServerWithAppPublicURL for tests that need it.
func newTestServerWithGitlabClient(t *testing.T, fake *gitlab.FakeClient) (*Server, *dbtest.FakeQuerier) {
	t.Helper()
	return newTestServerWithAppPublicURL(t, fake, "")
}

// newTestServerWithAppPublicURL is newTestServerWithGitlabClient, but with
// APP_PUBLIC_URL set to appPublicURL, so linked GitLab projects attempt
// webhook registration against fake.
func newTestServerWithAppPublicURL(t *testing.T, fake *gitlab.FakeClient, appPublicURL string) (*Server, *dbtest.FakeQuerier) {
	t.Helper()
	q := dbtest.New()
	cipher, err := crypto.New(testEncryptionKey)
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	projects := project.NewService(q)
	txRunner := dbtest.FakeTxRunner{Q: q}
	backlogs := backlog.NewService(q, txRunner, projects)
	apiTokens := apitoken.NewService(q, projects)
	users := user.NewService(q)
	projectMembers := projectmember.NewService(q, projects, users)
	clientFactory := func(string) gitlab.Client { return fake }
	gitlabConns := gitlabconn.NewService(q, projects, cipher, clientFactory)
	tasks := task.NewService(q, txRunner, projects, backlogs)
	return &Server{
		users:            users,
		projects:         projects,
		backlogs:         backlogs,
		apiTokens:        apiTokens,
		projectMembers:   projectMembers,
		projectInvites:   projectinvite.NewService(q, txRunner, projects),
		tasks:            tasks,
		taskDependencies: taskdependency.NewService(q, projects, tasks),
		taskComments:     taskcomment.NewService(q, txRunner, projects, tasks),
		mergeRequests:    mergerequest.NewService(q, projects),
		deliveryMetrics:  deliverymetrics.NewService(q, projects),
		flowMetrics:      flowmetrics.NewService(q, projects),
		velocity:         velocity.NewService(q, projects),
		gitlabConns:      gitlabConns,
		gitlabIdentities: gitlabidentity.NewService(q),
		linkedProjects:   linkedproject.NewService(q, txRunner, projects, gitlabConns, cipher, appPublicURL),
		projectSync:      projectsync.NewService(q, txRunner, projects, cipher, clientFactory),
		webhookEvents:    webhookevent.NewService(q, cipher),
		syncJobs:         syncjob.NewService(q, projects),
		webhookLimiter:   newSimpleRateLimiter(webhookRateLimit, webhookRateLimitWindow),
		tokenLimiter:     newSimpleRateLimiter(tokenRateLimit, tokenRateLimitWindow),
		authLimiter:      newSimpleRateLimiter(authRateLimit, authRateLimitWindow),
		sessions:         auth.NewSessionService(q, time.Hour),
		cookies:          cookieManager{secure: false},
		webBaseURL:       "http://localhost:3000",
		version:          "test",
		allowSignup:      true,
		sessionTTL:       time.Hour,
		cipher:           cipher,
	}, q
}

// loginSession seeds a user and issues a session for it, returning the
// user's ID and the raw session token.
func loginSession(t *testing.T, s *Server, q *dbtest.FakeQuerier) (uuid.UUID, string) {
	t.Helper()
	return loginSessionAs(t, s, q, "tester", "tester@example.com")
}

// loginSessionAs is loginSession for a named user, so a test that needs a
// second, unrelated session (an authz boundary, typically) can seed one
// without repeating the seed-then-create dance.
func loginSessionAs(t *testing.T, s *Server, q *dbtest.FakeQuerier, username, email string) (uuid.UUID, string) {
	t.Helper()
	u := q.SeedUser(username, email)
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

// postJSONFrom is postJSON, but with the request's RemoteAddr set to ip, so
// rate-limit tests can simulate distinct callers.
func postJSONFrom(t *testing.T, s *Server, path, ip string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestHandleLogin_RateLimitsExceedingAuthLimit(t *testing.T) {
	s, _ := newTestServer(t)
	s.authLimiter = newSimpleRateLimiter(2, time.Minute)

	login := func() *httptest.ResponseRecorder {
		return postJSONFrom(t, s, "/auth/login", "203.0.113.1:1", loginRequest{Identifier: "octocat", Password: "wrong-password"})
	}

	require.Equal(t, http.StatusUnauthorized, login().Code)
	require.Equal(t, http.StatusUnauthorized, login().Code)

	rec := login()
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestHandleLogin_RateLimitBucketsAreIndependentPerIP(t *testing.T) {
	s, _ := newTestServer(t)
	s.authLimiter = newSimpleRateLimiter(1, time.Minute)

	loginFrom := func(ip string) *httptest.ResponseRecorder {
		return postJSONFrom(t, s, "/auth/login", ip, loginRequest{Identifier: "octocat", Password: "wrong-password"})
	}

	require.Equal(t, http.StatusUnauthorized, loginFrom("203.0.113.1:1").Code)
	assert.Equal(t, http.StatusUnauthorized, loginFrom("203.0.113.2:1").Code, "a different IP must have its own rate-limit bucket")
	assert.Equal(t, http.StatusTooManyRequests, loginFrom("203.0.113.1:1").Code, "the first IP's own second request must still be blocked")
}

func TestHandleSignup_RateLimitsExceedingAuthLimit(t *testing.T) {
	s, _ := newTestServer(t)
	s.authLimiter = newSimpleRateLimiter(1, time.Minute)

	require.Equal(t, http.StatusCreated,
		postJSONFrom(t, s, "/auth/signup", "203.0.113.1:1", signupRequest{Username: "octocat", Email: "octocat@example.com", Password: "hunter22"}).Code)

	rec := postJSONFrom(t, s, "/auth/signup", "203.0.113.1:1", signupRequest{Username: "other", Email: "other@example.com", Password: "hunter22"})
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}
