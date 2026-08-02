package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenAuthorization_CrossProjectResourceGets404 pins issue #66's
// central threat model end to end, through the real router rather than a
// probe route: a write-scoped token issued for one project must never reach
// a task/backlog/dependency in a different project owned by the very same
// user, even though that owner-scoped check is exactly what every service
// method already performs.
func TestTokenAuthorization_CrossProjectResourceGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	ownProject := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	otherTask := q.SeedTask(otherProject.ID, owner.ID, "Other task")
	otherBacklog := q.SeedBacklog(otherProject.ID, "Other backlog")
	predecessor := q.SeedTask(otherProject.ID, owner.ID, "Predecessor")
	dep := q.SeedTaskDependency(predecessor.ID, otherTask.ID)

	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, ownProject.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET foreign task", http.MethodGet, "/api/v1/tasks/" + otherTask.ID.String()},
		{"PATCH foreign task", http.MethodPatch, "/api/v1/tasks/" + otherTask.ID.String()},
		{"DELETE foreign task", http.MethodDelete, "/api/v1/tasks/" + otherTask.ID.String()},
		{"POST foreign task close", http.MethodPost, "/api/v1/tasks/" + otherTask.ID.String() + "/close"},
		{"GET foreign backlog", http.MethodGet, "/api/v1/backlogs/" + otherBacklog.ID.String()},
		{"PATCH foreign backlog", http.MethodPatch, "/api/v1/backlogs/" + otherBacklog.ID.String()},
		{"DELETE foreign dependency", http.MethodDelete, "/api/v1/task-dependencies/" + dep.ID.String()},
		{"GET foreign project", http.MethodGet, "/api/v1/projects/" + otherProject.ID.String()},
		{"GET foreign project's tasks", http.MethodGet, "/api/v1/projects/" + otherProject.ID.String() + "/tasks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doBearerRequest(t, s, tt.method, tt.path, nil, raw)
			assert.Equal(t, http.StatusNotFound, rec.Code, "a token must not distinguish a foreign project's resource from a missing one")
		})
	}
}

// TestTokenAuthorization_ReadOnlyTokenGets403OnWrite pins the other half of
// issue #66's authorization model: a token's scope, not just its project,
// gates every mutation.
func TestTokenAuthorization_ReadOnlyTokenGets403OnWrite(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	backlogRow := q.SeedBacklog(p.ID, "Sprint 1")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"PATCH task", http.MethodPatch, "/api/v1/tasks/" + tsk.ID.String()},
		{"DELETE task", http.MethodDelete, "/api/v1/tasks/" + tsk.ID.String()},
		{"POST task close", http.MethodPost, "/api/v1/tasks/" + tsk.ID.String() + "/close"},
		{"POST create task", http.MethodPost, "/api/v1/projects/" + p.ID.String() + "/tasks"},
		{"PATCH backlog", http.MethodPatch, "/api/v1/backlogs/" + backlogRow.ID.String()},
		{"DELETE backlog", http.MethodDelete, "/api/v1/backlogs/" + backlogRow.ID.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doBearerRequest(t, s, tt.method, tt.path, nil, raw)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		})
	}

	// The same read-only token must still be able to read.
	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String(), nil, raw)
	assert.Equal(t, http.StatusOK, rec.Code, "a read-scoped token must still reach GET routes")
}

// TestTokenAuthorization_WriteScopedTokenCanActOnOwnProject is the positive
// control for TestTokenAuthorization_CrossProjectResourceGets404 and
// TestTokenAuthorization_ReadOnlyTokenGets403OnWrite: a write-scoped token
// acting on its own project's own task must succeed end to end.
func TestTokenAuthorization_WriteScopedTokenCanActOnOwnProject(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/close", nil, raw)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "closed", body["status"])
}

// TestHandleListProjects_BearerAuthReturnsOnlyTokenProject pins issue #66's
// explicit requirement that GET /projects, unlike every other bearer-facing
// handler, cannot lean on the owner-scoped List a session request uses — a
// token's owner may have other projects the token itself was never issued
// for.
func TestHandleListProjects_BearerAuthReturnsOnlyTokenProject(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p1 := q.SeedProject(owner.ID, "Alpha")
	q.SeedProject(owner.ID, "Beta")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p1.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/projects", nil, raw)
	require.Equal(t, http.StatusOK, rec.Code)
	var projects []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &projects))
	require.Len(t, projects, 1, "a token must only ever see its own project, never every project its owner has")
	assert.Equal(t, p1.ID.String(), projects[0]["id"])
}

// TestTokenAuthorization_GitlabConnectionUnreachableByToken pins that a
// token can never read the encrypted GitLab PAT behind
// /gitlab-connection*, regardless of scope — these routes are session-only,
// permanently, per server.go's allowlist.
func TestTokenAuthorization_GitlabConnectionUnreachableByToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/projects/" + p.ID.String() + "/gitlab-connection"},
		{http.MethodPut, "/api/v1/projects/" + p.ID.String() + "/gitlab-connection"},
		{http.MethodDelete, "/api/v1/projects/" + p.ID.String() + "/gitlab-connection"},
		{http.MethodPost, "/api/v1/projects/" + p.ID.String() + "/gitlab-connection/test"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := doBearerRequest(t, s, tt.method, tt.path, nil, raw)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "a write-scoped token must still never reach gitlab-connection*")
		})
	}
}

// TestTokenAuthorization_APITokensUnreachableByToken pins that a token can
// never mint or revoke API tokens — that would be privilege escalation.
func TestTokenAuthorization_APITokensUnreachableByToken(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	created, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/api-tokens", nil, raw)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doBearerRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/api-tokens", createAPITokenRequest{Name: "Escalate"}, raw)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doBearerRequest(t, s, http.MethodDelete, "/api/v1/api-tokens/"+created.ID.String(), nil, raw)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestTokenAuthorization_SyncRunsCrossProjectGets404 covers the same
// project-boundary gap as TestTokenAuthorization_CrossProjectResourceGets404,
// but for GET /linked-gitlab-projects/{linkID}/sync-runs — not called out by
// name in issue #66, but exposed to the exact same "same owner, different
// project" leak every other single-resource route needed
// requireTokenResourceProject for.
func TestTokenAuthorization_SyncRunsCrossProjectGets404(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, session := loginSession(t, s, q)
	ownProject := q.SeedProject(ownerID, "Alpha")
	linkID := setUpLinkedProject(t, s, session, ownProject.ID.String())

	otherProject := q.SeedProject(ownerID, "Beta")
	_, raw, err := s.apiTokens.Create(context.Background(), ownerID, otherProject.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+linkID+"/sync-runs", nil, raw)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestAPIToken_IssueWriteRevokeRoundtrip is the full lifecycle acceptance
// test issue #66 asks for: issue a write-scoped token over HTTP, use it to
// write, revoke it, and confirm it is rejected afterward. Per
// docs/testing.md's layering, this belongs at the HTTP layer (through the
// router, against dbtest.FakeQuerier) like every other authn/authz test in
// this package, rather than gated behind the `integration` build tag —
// nothing here depends on real Postgres.
func TestAPIToken_IssueWriteRevokeRoundtrip(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, session := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/api-tokens",
		createAPITokenRequest{Name: "CI bot", Scopes: []string{apitoken.ScopeWrite}}, session)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created createAPITokenResponse
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.ElementsMatch(t, []string{apitoken.ScopeRead, apitoken.ScopeWrite}, created.Scopes, "write must imply read")

	writeRec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/close", nil, created.Token)
	require.Equal(t, http.StatusOK, writeRec.Code, writeRec.Body.String())

	revokeRec := doRequest(t, s, http.MethodDelete, "/api/v1/api-tokens/"+created.ID.String(), nil, session)
	require.Equal(t, http.StatusNoContent, revokeRec.Code)

	postRevokeRec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/reopen", nil, created.Token)
	assert.Equal(t, http.StatusUnauthorized, postRevokeRec.Code, "a revoked token must be rejected, not just scope-limited")
}
