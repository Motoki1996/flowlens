package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/task"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bearerTestRouter mounts requireBearerAuth and requireTokenProjectMatch in
// front of a stub handler that echoes the token's project ID, the same
// composition server.go's project-scoped, bearer-reachable routes use
// (issue #66's route allowlist).
func bearerTestRouter(s *Server) chi.Router {
	r := chi.NewRouter()
	r.Route("/projects/{projectID}/probe", func(pr chi.Router) {
		pr.Use(s.requireBearerAuth)
		pr.Use(requireTokenProjectMatch)
		pr.Get("/", func(w http.ResponseWriter, r *http.Request) {
			projectID, _ := tokenProjectFromContext(r.Context())
			writeJSON(w, http.StatusOK, map[string]string{"projectId": projectID.String()})
		})
	})
	return r
}

func TestRequireBearerAuth_NoHeader(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/projects/"+uuid.New().String()+"/probe/", nil)
	rec := httptest.NewRecorder()
	bearerTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireBearerAuth_UnknownToken(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/projects/"+uuid.New().String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer bogus-token")
	rec := httptest.NewRecorder()
	bearerTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireBearerAuth_ExpiredToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	past := time.Now().Add(-time.Hour)
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, &past)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID.String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	bearerTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireBearerAuth_RevokedToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	token, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)
	require.NoError(t, s.apiTokens.Delete(context.Background(), owner.ID, token.ID))

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID.String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	bearerTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireBearerAuth_ValidTokenSetsProjectInContext(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID.String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	bearerTestRouter(s).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), p.ID.String())
}

func TestRequireTokenProjectMatch_ReturnsNotFoundForForeignProject(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	ownProject := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, ownProject.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/projects/"+otherProject.ID.String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	bearerTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRequireAuthOrBearer_AcceptsSessionCookie(t *testing.T) {
	s, q := newTestServer(t)
	_, session := loginSession(t, s, q)

	r := chi.NewRouter()
	r.With(s.requireAuthOrBearer).Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "internal_error", "expected a user in context")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"username": u.Username})
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireAuthOrBearer_AcceptsBearerToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(s.requireAuthOrBearer).Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := tokenProjectFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "internal_error", "expected a token project in context")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"projectId": projectID.String()})
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), p.ID.String())
}

func TestRequireBearerAuth_RateLimitsExceedingTokenLimit(t *testing.T) {
	s, q := newTestServer(t)
	s.tokenLimiter = newSimpleRateLimiter(2, time.Minute)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)

	req := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID.String()+"/probe/", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		bearerTestRouter(s).ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, req().Code)
	require.Equal(t, http.StatusOK, req().Code)

	rec := req()
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.NotContains(t, rec.Body.String(), raw, "the raw token must never appear in the rate-limit response")
}

func TestRequireBearerAuth_RateLimitBucketsAreIndependentPerToken(t *testing.T) {
	s, q := newTestServer(t)
	s.tokenLimiter = newSimpleRateLimiter(1, time.Minute)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, rawA, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "Bot A", []string{"read"}, nil)
	require.NoError(t, err)
	_, rawB, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "Bot B", []string{"read"}, nil)
	require.NoError(t, err)

	get := func(raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID.String()+"/probe/", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		bearerTestRouter(s).ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, get(rawA).Code)
	assert.Equal(t, http.StatusOK, get(rawB).Code, "a different token must have its own rate-limit bucket")
	assert.Equal(t, http.StatusTooManyRequests, get(rawA).Code, "token A's own second request must still be blocked")
}

func TestRequireBearerAuth_RateLimitRecoversAfterWindow(t *testing.T) {
	s, q := newTestServer(t)
	s.tokenLimiter = newSimpleRateLimiter(1, time.Millisecond)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)

	req := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/projects/"+p.ID.String()+"/probe/", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		bearerTestRouter(s).ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusOK, req().Code)
	time.Sleep(5 * time.Millisecond)
	assert.Equal(t, http.StatusOK, req().Code, "a new window must reset the token's budget")
}

func TestRequireAuthOrBearer_SessionAuthIsNotRateLimited(t *testing.T) {
	s, q := newTestServer(t)
	s.tokenLimiter = newSimpleRateLimiter(1, time.Minute)
	_, session := loginSession(t, s, q)

	r := chi.NewRouter()
	r.With(s.requireAuthOrBearer).Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "session-authenticated requests must never be subject to tokenLimiter")
	}
}

func TestRequireAuthOrBearer_RejectsInvalidCookieWithoutFallingBackToBearer(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(s.requireAuthOrBearer).Get("/whoami", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bogus-session"})
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestRequireBearerAuth_ResolvesTokenOwnerAsUser pins issue #66's design: a
// bearer token resolves to a real user.User (its project's owner) in
// userContextKey, the same key requireAuth uses — this is what lets every
// existing owner-scoped handler serve a token request with no changes of
// its own.
func TestRequireBearerAuth_ResolvesTokenOwnerAsUser(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(s.requireBearerAuth).Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		u, ok := userFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "internal_error", "expected a user in context")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"userId": u.ID.String(), "username": u.Username})
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), owner.ID.String())
	assert.Contains(t, rec.Body.String(), "octocat")
}

func TestRequireTokenScope_ForbidsTokenWithoutScope(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(s.requireBearerAuth, requireTokenScope(apitoken.ScopeWrite)).Post("/probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireTokenScope_AllowsTokenWithScope(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(s.requireBearerAuth, requireTokenScope(apitoken.ScopeWrite)).Post("/probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireTokenScope_NoOpForSessionAuth(t *testing.T) {
	s, q := newTestServer(t)
	_, session := loginSession(t, s, q)

	r := chi.NewRouter()
	r.With(s.requireAuthOrBearer, requireTokenScope(apitoken.ScopeWrite)).Post("/probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "a session-authenticated request must never be scope-limited")
}

// taskResourceTestRouter mounts requireBearerAuth and
// requireTokenResourceProject("taskID", ...) in front of a stub handler, the
// same composition server.go's single-task bearer-reachable routes use.
func taskResourceTestRouter(s *Server) chi.Router {
	r := chi.NewRouter()
	r.Route("/tasks/{taskID}/probe", func(pr chi.Router) {
		pr.Use(s.requireBearerAuth)
		pr.Use(requireTokenResourceProject("taskID", task.ErrNotFound, s.tasks.ProjectID))
		pr.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
	return r
}

func TestRequireTokenResourceProject_AllowsMatchingProject(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+tsk.ID.String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	taskResourceTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequireTokenResourceProject_ReturnsNotFoundForForeignProject pins
// issue #66's central threat: since a bearer token now resolves to a real
// user (its project's owner), the owner-scoped check every service method
// already performs is not enough on its own — a task in a *different*
// project owned by that same user must still be unreachable.
func TestRequireTokenResourceProject_ReturnsNotFoundForForeignProject(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	ownProject := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	otherTask := q.SeedTask(otherProject.ID, owner.ID, "Other task")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, ownProject.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+otherTask.ID.String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	taskResourceTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRequireTokenResourceProject_ReturnsNotFoundForUnknownResource(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+uuid.New().String()+"/probe/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	taskResourceTestRouter(s).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRequireTokenResourceProject_NoOpForSessionAuth(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")

	r := chi.NewRouter()
	r.Route("/tasks/{taskID}/probe", func(pr chi.Router) {
		pr.Use(s.requireAuthOrBearer)
		pr.Use(requireTokenResourceProject("taskID", task.ErrNotFound, s.tasks.ProjectID))
		pr.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks/"+tsk.ID.String()+"/probe/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "a session-authenticated request must never be resource-boundary limited")
}
