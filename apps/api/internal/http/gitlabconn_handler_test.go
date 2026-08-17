package http

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "glpat-abcdefghijklmnopqrst"

func TestHandlePutGitlabConnection_SavesAndNeverReturnsTheToken(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	rec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com/", Token: testToken}, token)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), testToken, "the plaintext token must never appear in a response")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotContains(t, body, "token")
	assert.NotContains(t, body, "encryptedToken")
	assert.Equal(t, "qrst", body["tokenLastFour"])
	assert.Equal(t, "https://gitlab.example.com", body["baseUrl"])
	assert.Equal(t, "octocat", body["tokenGitlabUsername"])
	assert.Equal(t, true, body["verified"])
}

// TestHandlePutGitlabConnection_MemberRoleGets403 covers issue #99's other
// named completion criterion: a member (below owner) gets 403 on the GitLab
// connection endpoint — the credential itself stays owner-only even though
// a member can use an already-connected project to link/sync.
func TestHandlePutGitlabConnection_MemberRoleGets403(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	memberID, memberToken := loginSession(t, s, q)
	q.SeedProjectMember(p.ID, memberID, "member")

	rec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+p.ID.String()+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com/", Token: testToken}, memberToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandlePutGitlabConnection_RejectsInvalidBaseURL(t *testing.T) {
	s, q := newTestServerWithGitlabClient(t, &gitlab.FakeClient{})
	ownerID, token := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	rec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "not-a-url", Token: testToken}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePutGitlabConnection_ReturnsDistinctCodesForUnreachableVsInvalidToken(t *testing.T) {
	tests := []struct {
		name     string
		verErr   error
		wantCode string
	}{
		{"connection refused is unreachable", assert.AnError, "unreachable"},
		{"a rejected certificate is a tls error", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "tls_error"},
		{"401 is an invalid token", &gitlab.APIError{StatusCode: 401, Body: "unauthorized"}, "invalid_token"},
		{"403 is insufficient scope", &gitlab.APIError{StatusCode: 403, Body: "forbidden"}, "insufficient_scope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &gitlab.FakeClient{Err: tt.verErr}
			s, q := newTestServerWithGitlabClient(t, fake)
			ownerID, token := loginSession(t, s, q)
			id := q.SeedProject(ownerID, "Alpha").ID.String()

			rec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
				putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com", Token: testToken}, token)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			var body errorBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantCode, body.Error.Code)
			assert.NotContains(t, rec.Body.String(), testToken)

			// A failed verification must not save a connection.
			assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/gitlab-connection", nil, token).Code)
		})
	}
}

func TestHandlePutGitlabConnection_ForeignProjectGets404(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, _ := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(t.Context(), other.ID)
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com", Token: testToken}, otherToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetGitlabConnection(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	t.Run("404 before a connection is saved", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/gitlab-connection", nil, token).Code)
	})

	saveRec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com", Token: testToken}, token)
	require.Equal(t, http.StatusOK, saveRec.Code)

	t.Run("200 with only the last four characters afterwards", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/gitlab-connection", nil, token)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Body.String(), testToken)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "qrst", body["tokenLastFour"])
	})

	t.Run("a non-owner gets 404, not the other user's connection", func(t *testing.T) {
		other := q.SeedUser("intruder", "intruder@example.com")
		otherToken, err := s.sessions.Create(t.Context(), other.ID)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/gitlab-connection", nil, otherToken).Code)
	})
}

func TestHandleTestGitlabConnection_ReVerifiesAndPersists(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	require.Equal(t, http.StatusOK, doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com", Token: testToken}, token).Code)

	t.Run("success", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/gitlab-connection/test", nil, token)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, true, body["verified"])
	})

	t.Run("a rejected token is reported as 422 invalid_token, distinct from unreachable", func(t *testing.T) {
		fake.AuthenticatedUser = nil
		fake.Err = &gitlab.APIError{StatusCode: 401, Body: "unauthorized"}

		rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+id+"/gitlab-connection/test", nil, token)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		var body errorBody
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "invalid_token", body.Error.Code)
	})
}

func TestHandleDeleteGitlabConnection(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 1, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	id := q.SeedProject(ownerID, "Alpha").ID.String()

	require.Equal(t, http.StatusOK, doRequest(t, s, http.MethodPut, "/api/v1/projects/"+id+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com", Token: testToken}, token).Code)

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(t.Context(), other.ID)
	require.NoError(t, err)

	// A non-owner gets 404 and the connection survives.
	require.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id+"/gitlab-connection", nil, otherToken).Code)
	require.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/gitlab-connection", nil, token).Code)

	require.Equal(t, http.StatusNoContent, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id+"/gitlab-connection", nil, token).Code)
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/projects/"+id+"/gitlab-connection", nil, token).Code)

	// Deleting twice is reported as "not found", not as success.
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+id+"/gitlab-connection", nil, token).Code)
}
