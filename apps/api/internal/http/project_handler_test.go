package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flowlens/api/internal/issuesync"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doRequest sends a request through the router, optionally with a JSON body
// and a session cookie.
func doRequest(t *testing.T, s *Server, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestHandleListProjects_NoCookie(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleListProjects_ScopesToOwner(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	q.SeedProject(ownerID, "Alpha")
	q.SeedProject(ownerID, "Beta")

	other := q.SeedUser("intruder", "intruder@example.com")
	q.SeedProject(other.ID, "Gamma")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects", nil, ownerToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body, 2)
}

func TestHandleCreateProject(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", createProjectRequest{Name: "Alpha", Description: "desc"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Alpha", body["name"])
	assert.Equal(t, "desc", body["description"])

	// IDs are serialised in the canonical UUID form, so a client can round
	// trip one straight back into the URL.
	id, ok := body["id"].(string)
	require.True(t, ok)
	_, err := uuid.Parse(id)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id, nil, token).Code)
}

func TestHandleCreateProject_RejectsDuplicateName(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", createProjectRequest{Name: "Alpha"}, token)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleCreateProject_RejectsInvalidName(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects", createProjectRequest{Name: "   "}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		token    string
		wantCode int
	}{
		{"owner can view", "/api/v1/projects/" + id, ownerToken, http.StatusOK},
		{"other user gets 404, not 403", "/api/v1/projects/" + id, otherToken, http.StatusNotFound},
		{"unknown id gets 404", "/api/v1/projects/" + uuid.New().String(), ownerToken, http.StatusNotFound},
		{"malformed id gets 404", "/api/v1/projects/not-a-uuid", ownerToken, http.StatusNotFound},
		{"no auth gets 401", "/api/v1/projects/" + id, "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodGet, tt.path, nil, tt.token)
			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestHandleGetProject_IncludesFailedSyncTaskCount(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	okTask := q.SeedTask(p.ID, ownerID, "Fine")
	failedTask := q.SeedTask(p.ID, ownerID, "Broken")
	q.SeedSyncJobForTask(failedTask.ID, p.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")
	_ = okTask

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, float64(1), body["failedSyncTaskCount"])
}

func TestHandleListProjects_FailedSyncFiltersToProjectsWithFailures(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)

	clean := q.SeedProject(ownerID, "Clean")
	q.SeedTask(clean.ID, ownerID, "Fine")

	broken := q.SeedProject(ownerID, "Broken")
	failedTask := q.SeedTask(broken.ID, ownerID, "Broken task")
	q.SeedSyncJobForTask(failedTask.ID, broken.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects?failedSync=true", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Broken", body[0]["name"])
	assert.Equal(t, float64(1), body[0]["failedSyncTaskCount"])
}

func TestHandleUpdateProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	t.Run("other user gets 404 and does not modify the project", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+id, updateProjectRequest{Name: "Hijacked"}, otherToken)
		require.Equal(t, http.StatusNotFound, rec.Code)

		rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id, nil, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Alpha", body["name"])
	})

	t.Run("owner can update", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+id, updateProjectRequest{Name: "Renamed", Description: "new"}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Renamed", body["name"])
		assert.Equal(t, "new", body["description"])
	})
}

func TestHandleDeleteProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	// A non-owner gets 404 and the project survives.
	require.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id, nil, otherToken).Code)
	require.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id, nil, ownerToken).Code)

	require.Equal(t, http.StatusNoContent, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id, nil, ownerToken).Code)
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id, nil, ownerToken).Code)

	// Deleting twice is reported as "not found", not as success.
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id, nil, ownerToken).Code)
}
