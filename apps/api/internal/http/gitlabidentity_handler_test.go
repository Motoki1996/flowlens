package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePutMyGitlabIdentity(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodPut, "/api/v1/me/gitlab-identities",
		putGitlabIdentityRequest{GitlabBaseURL: "https://gitlab.example.com", GitlabUserID: 7, GitlabUsername: "octocat"}, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "https://gitlab.example.com", body["gitlabBaseUrl"])
	assert.Equal(t, float64(7), body["gitlabUserId"])
	assert.Equal(t, "octocat", body["gitlabUsername"])
}

func TestHandlePutMyGitlabIdentity_RejectsEmptyBaseURL(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodPut, "/api/v1/me/gitlab-identities",
		putGitlabIdentityRequest{GitlabBaseURL: "", GitlabUserID: 7}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListMyGitlabIdentities(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/me/gitlab-identities", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "[]", rec.Body.String())

	doRequest(t, s, http.MethodPut, "/api/v1/me/gitlab-identities",
		putGitlabIdentityRequest{GitlabBaseURL: "https://gitlab.example.com", GitlabUserID: 7, GitlabUsername: "octocat"}, token)

	rec = doRequest(t, s, http.MethodGet, "/api/v1/me/gitlab-identities", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "https://gitlab.example.com", body[0]["gitlabBaseUrl"])
}
