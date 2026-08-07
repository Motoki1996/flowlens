package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireCSRF_MissingHeaderRejected(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+id, nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	// Deliberately no X-CSRF-Token header.
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireCSRF_MismatchedTokenRejected(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+id, nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	req.Header.Set(csrfHeaderName, "some-other-value")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireCSRF_MatchingTokenAccepted(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id, map[string]any{"title": "Renamed"}, token)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequireCSRF_GetIsExempt confirms a safe method never needs the token,
// so read-only screens keep working the moment the session cookie exists.
func TestRequireCSRF_GetIsExempt(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequireCSRF_BearerTokenExempt is the regression test the issue asks
// for: a project API token authenticates every request explicitly via the
// Authorization header, so it is never subject to CSRF and must keep working
// without an X-CSRF-Token header or a CSRF cookie at all.
func TestRequireCSRF_BearerTokenExempt(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"write"}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/"+id, strings.NewReader(`{"title":"Renamed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestRequireCSRF_WebhookReceiverUnaffected confirms the CSRF check never
// applies to the GitLab webhook receiver, which authenticates via a
// token-header (not a cookie) and sits outside both the session-only and
// session-or-bearer route groups requireCSRF is mounted on.
func TestRequireCSRF_WebhookReceiverUnaffected(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab/"+"00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no CSRF cookie/header and no valid webhook secret token:
	// the response must come from webhook auth, never csrf_token_mismatch.
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	assert.NotEqual(t, http.StatusForbidden, rec.Code)
}
