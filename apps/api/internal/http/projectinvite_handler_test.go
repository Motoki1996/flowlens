package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/projectinvite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createInvite issues an invite through the API and returns its raw token,
// the way a project owner actually gets one.
func createInvite(t *testing.T, s *Server, projectID, token, role string) (string, string) {
	t.Helper()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/invites",
		createProjectInviteRequest{Role: role}, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	raw, _ := body["token"].(string)
	id, _ := body["id"].(string)
	require.NotEmpty(t, raw, "the raw token is only ever returned here")
	return raw, id
}

func TestHandleCreateProjectInvite_ReturnsTheRawTokenOnce(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	raw, _ := createInvite(t, s, p.ID.String(), ownerToken, "viewer")

	// Listing must never repeat it — only the prefix, for telling invites apart.
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/invites", nil, ownerToken)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), raw)

	var invites []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &invites))
	require.Len(t, invites, 1)
	assert.Equal(t, "viewer", invites[0]["role"])
	assert.Equal(t, projectinvite.StatusPending, invites[0]["status"])
}

func TestHandleProjectInvites_AreOwnerOnly(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	_, memberToken := loginSessionAs(t, s, q, "member", "member@example.com")
	memberID, _ := loginSessionAs(t, s, q, "member2", "member2@example.com")
	q.SeedProjectMember(p.ID, memberID, "member")

	raw, inviteID := createInvite(t, s, p.ID.String(), ownerToken, "member")
	require.NotEmpty(t, raw)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"list", http.MethodGet, "/api/v1/projects/" + p.ID.String() + "/invites", nil},
		{"create", http.MethodPost, "/api/v1/projects/" + p.ID.String() + "/invites", createProjectInviteRequest{Role: "member"}},
		{"revoke", http.MethodDelete, "/api/v1/invites/" + inviteID, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, tt.method, tt.path, tt.body, memberToken)
			assert.Equal(t, http.StatusNotFound, rec.Code,
				"a non-member must not be able to reach a project's invites, nor learn it exists")
		})
	}
}

// TestHandlePreviewInvite_IsUnauthenticated pins the route an invitee with
// no account has to be able to reach.
func TestHandlePreviewInvite_IsUnauthenticated(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	raw, _ := createInvite(t, s, p.ID.String(), ownerToken, "member")

	rec := doRequest(t, s, http.MethodGet, "/auth/invites/"+raw, nil, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var preview map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
	assert.Equal(t, "Alpha", preview["projectName"])
	assert.Equal(t, "member", preview["role"])
}

func TestHandlePreviewInvite_UnknownTokenIsNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doRequest(t, s, http.MethodGet, "/auth/invites/fli_never-existed", nil, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestSignupWithInvite_WorksWithRegistrationClosed is the whole point of
// issue #211: an instance hardened with ALLOW_SIGNUP=false can still onboard
// the person an owner invited, and only them.
func TestSignupWithInvite_WorksWithRegistrationClosed(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	raw, _ := createInvite(t, s, p.ID.String(), ownerToken, "member")
	s.allowSignup = false

	rec := postJSON(t, s, "/auth/signup", signupRequest{
		Username: "invitee", Email: "invitee@example.com", Password: "hunter22", InviteToken: raw,
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// The new account is signed in and can see the project it was invited to.
	c := sessionCookie(rec)
	require.NotNil(t, c)
	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects", nil, c.Value)
	require.Equal(t, http.StatusOK, listRec.Code)
	var projects []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &projects))
	require.Len(t, projects, 1)
	assert.Equal(t, p.ID.String(), projects[0]["id"])

	// Registration is still closed to anyone without an invite.
	plain := postJSON(t, s, "/auth/signup", signupRequest{
		Username: "stranger", Email: "stranger@example.com", Password: "hunter22",
	})
	assert.Equal(t, http.StatusForbidden, plain.Code)

	// And the invite is spent, so the link cannot admit a second person.
	reused := postJSON(t, s, "/auth/signup", signupRequest{
		Username: "second", Email: "second@example.com", Password: "hunter22", InviteToken: raw,
	})
	assert.Equal(t, http.StatusNotFound, reused.Code)
}

// TestSignupWithInvalidInvite_CreatesNoAccount pins that the invite is
// checked before anything is created: a bad invite must not leave an orphan
// account behind on an instance that has closed registration.
func TestSignupWithInvalidInvite_CreatesNoAccount(t *testing.T) {
	s, _ := newTestServer(t)
	s.allowSignup = false

	rec := postJSON(t, s, "/auth/signup", signupRequest{
		Username: "invitee", Email: "invitee@example.com", Password: "hunter22", InviteToken: "fli_never-existed",
	})
	require.Equal(t, http.StatusNotFound, rec.Code)

	login := postJSON(t, s, "/auth/login", loginRequest{Identifier: "invitee", Password: "hunter22"})
	assert.Equal(t, http.StatusUnauthorized, login.Code, "no account may have been created")
}

func TestHandleAcceptInvite_JoinsAnExistingAccount(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	raw, _ := createInvite(t, s, p.ID.String(), ownerToken, "member")
	_, joinerToken := loginSessionAs(t, s, q, "joiner", "joiner@example.com")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/invites/accept", acceptProjectInviteRequest{Token: raw}, joinerToken)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects", nil, joinerToken)
	require.Equal(t, http.StatusOK, listRec.Code)
	var projects []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &projects))
	require.Len(t, projects, 1)

	// Accepting twice is a conflict, not a second membership.
	again := doRequest(t, s, http.MethodPost, "/api/v1/invites/accept", acceptProjectInviteRequest{Token: raw}, joinerToken)
	assert.Equal(t, http.StatusNotFound, again.Code)
}

func TestHandleAcceptInvite_ExistingMemberConflicts(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	raw, _ := createInvite(t, s, p.ID.String(), ownerToken, "member")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/invites/accept", acceptProjectInviteRequest{Token: raw}, ownerToken)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// TestProjectInvites_UnreachableByToken pins that a project API token can
// never mint or revoke an invite: who can reach a project at all is a
// project-management decision, the same boundary that keeps /api-tokens and
// member management off the bearer allowlist (server.go).
func TestProjectInvites_UnreachableByToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/projects/" + p.ID.String() + "/invites"},
		{http.MethodPost, "/api/v1/projects/" + p.ID.String() + "/invites"},
		{http.MethodPost, "/api/v1/invites/accept"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := doBearerRequest(t, s, tt.method, tt.path, createProjectInviteRequest{Role: "owner"}, raw)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}
