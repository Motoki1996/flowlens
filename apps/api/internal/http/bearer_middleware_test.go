package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bearerTestRouter mounts requireBearerAuth and requireTokenProjectMatch in
// front of a stub handler that echoes the token's project ID, the same
// composition a future project-scoped, AI-facing endpoint would use
// (docs/plans/issue-sync.md "AI-facing"; no such endpoint exists yet, so
// this is the only place that pipeline runs end to end).
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
