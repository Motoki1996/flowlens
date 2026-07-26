package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/project"
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
	owner, ownerToken := loginSession(t, s, q)
	q.SeedProject(owner.ID, "Alpha")
	q.SeedProject(owner.ID, "Beta")

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
	assert.NotEmpty(t, body["id"])
}

func TestHandleCreateProject_RejectsDuplicateName(t *testing.T) {
	s, q := newTestServer(t)
	u, token := loginSession(t, s, q)
	q.SeedProject(u.ID, "Alpha")

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
	owner, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(owner.ID, "Alpha")
	id := project.FromDB(p).ID

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
		{"unknown id gets 404", "/api/v1/projects/" + strings.Repeat("0", 32), ownerToken, http.StatusNotFound},
		{"no auth gets 401", "/api/v1/projects/" + id, "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodGet, tt.path, nil, tt.token)
			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestHandleUpdateProject(t *testing.T) {
	s, q := newTestServer(t)
	owner, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(owner.ID, "Alpha")
	id := project.FromDB(p).ID

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	t.Run("other user gets 404, not 403", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+id, updateProjectRequest{Name: "Renamed"}, otherToken)
		assert.Equal(t, http.StatusNotFound, rec.Code)
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
	owner, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(owner.ID, "Alpha")
	id := project.FromDB(p).ID

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id, nil, otherToken).Code)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id, nil, ownerToken)
	require.Equal(t, http.StatusNoContent, rec.Code)

	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id, nil, ownerToken).Code)
}
