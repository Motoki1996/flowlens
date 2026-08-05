package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/gitlab"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setUpConnectedProject seeds a project owned by the logged-in user with a
// saved GitLab connection, ready for linking a GitLab project.
func setUpConnectedProject(t *testing.T, s *Server, token, projectID string) {
	t.Helper()
	rec := doRequest(t, s, http.MethodPut, "/api/v1/projects/"+projectID+"/gitlab-connection",
		putGitlabConnectionRequest{BaseURL: "https://gitlab.example.com", Token: testToken}, token)
	require.Equal(t, http.StatusOK, rec.Code, "expected the gitlab connection to save: %s", rec.Body.String())
}

func TestHandleCreateLinkedGitlabProject_LinksAndReturnsMetadataFromGitlab(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo", WebURL: "https://gitlab.example.com/group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "group/demo", body["pathWithNamespace"])
	assert.Equal(t, true, body["isDefault"])
}

func TestHandleCreateLinkedGitlabProject_LabelsScopeWithoutLabelsIs422(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "labels"}, token)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestHandleCreateLinkedGitlabProject_DuplicateGetsConflict(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	req := createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}
	require.Equal(t, http.StatusCreated, doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects", req, token).Code)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects", req, token)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleListLinkedGitlabProjects_ForeignProjectGets404(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	owner := q.SeedUser("owner", "owner@example.com")
	projectID := q.SeedProject(owner.ID, "Alpha").ID.String()
	_, token := loginSession(t, s, q) // a different user

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+projectID+"/linked-gitlab-projects", nil, token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetLinkedGitlabProject_ReturnsTheLink(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+projectID+"/linked-gitlab-projects/"+linkID, nil, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, linkID, body["id"])
	assert.Equal(t, "group/demo", body["pathWithNamespace"])
}

// A link is reached through a project's URL, so the project boundary is part
// of the contract: another user's link, another of the caller's own projects,
// and an unknown ID are all indistinguishable 404s.
func TestHandleGetLinkedGitlabProject_ForeignLinkGets404(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)
	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	otherOwnerID, otherToken := loginSessionAs(t, s, q, "intruder", "intruder@example.com")
	otherProjectID := q.SeedProject(otherOwnerID, "Theirs").ID.String()
	// Another of the caller's own projects, for the cross-project case.
	siblingProjectID := q.SeedProject(ownerID, "Beta").ID.String()

	tests := []struct {
		name    string
		path    string
		session string
	}{
		{"another user's link", "/api/v1/projects/" + otherProjectID + "/linked-gitlab-projects/" + linkID, otherToken},
		{"the caller's other project", "/api/v1/projects/" + siblingProjectID + "/linked-gitlab-projects/" + linkID, token},
		{"unknown link", "/api/v1/projects/" + projectID + "/linked-gitlab-projects/" + uuid.NewString(), token},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, tt.path, nil, tt.session).Code)
		})
	}
}

func TestHandleUpdateLinkedGitlabProject_ChangesSyncScope(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/linked-gitlab-projects/"+linkID,
		updateLinkedGitlabProjectRequest{SyncScope: "labels", SyncLabels: []string{"bug"}}, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "labels", body["syncScope"])
}

func TestHandleDeleteLinkedGitlabProject_RemovesLinkOnly(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/linked-gitlab-projects/"+linkID, nil, token)
	require.Equal(t, http.StatusNoContent, rec.Code)

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+projectID+"/linked-gitlab-projects", nil, token)
	require.Equal(t, http.StatusOK, listRec.Code)
	var links []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &links))
	assert.Empty(t, links)
}

// TestHandleCreateLinkedGitlabProject_RegistersWebhookWithoutExposingSecret
// covers issue #18's acceptance criteria: creating a link with
// APP_PUBLIC_URL configured registers its webhook, and the secret never
// appears in the response.
func TestHandleCreateLinkedGitlabProject_RegistersWebhookWithoutExposingSecret(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
		Hook:              &gitlab.ProjectHook{ID: 501},
	}
	s, q := newTestServerWithAppPublicURL(t, fake, "https://flowlens.example.com")
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotContains(t, strings.ToLower(rec.Body.String()), "secret")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "registered", body["webhookStatus"])
	assert.NotEmpty(t, body["webhookRegisteredAt"])
}

// TestHandleRegisterLinkedGitlabProjectWebhook_RepairIsIdempotent covers
// issue #18's acceptance criterion that repeating the repair endpoint never
// duplicates the GitLab-side hook.
func TestHandleRegisterLinkedGitlabProjectWebhook_RepairIsIdempotent(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
		Hook:              &gitlab.ProjectHook{ID: 501},
	}
	appPublicURL := "https://flowlens.example.com"
	s, q := newTestServerWithAppPublicURL(t, fake, appPublicURL)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	// Simulate the hook now existing on GitLab, as ListProjectHooks would
	// report after the create above.
	fake.Hooks = []gitlab.ProjectHook{{ID: 501, URL: appPublicURL + "/webhooks/gitlab/" + linkID}}

	for i := 0; i < 2; i++ {
		rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+linkID+"/webhook", nil, token)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "registered", body["webhookStatus"])
	}

	var createCalls, updateCalls int
	for _, call := range fake.CallLog {
		switch call.Method {
		case "CreateProjectHook":
			createCalls++
		case "UpdateProjectHook":
			updateCalls++
		}
	}
	assert.Equal(t, 1, createCalls, "repairing an already-registered webhook must never create a duplicate hook")
	assert.Equal(t, 2, updateCalls)
}

// TestHandleDeleteGitlabConnection_DeregistersEveryLinkedWebhook covers
// issue #18's requirement that disconnecting GitLab entirely deregisters
// the webhooks of every GitLab project linked through it.
func TestHandleDeleteGitlabConnection_DeregistersEveryLinkedWebhook(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
		Hook:              &gitlab.ProjectHook{ID: 501},
	}
	s, q := newTestServerWithAppPublicURL(t, fake, "https://flowlens.example.com")
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/projects/"+projectID+"/gitlab-connection", nil, token)
	require.Equal(t, http.StatusNoContent, rec.Code)

	var sawDeleteHook bool
	for _, call := range fake.CallLog {
		if call.Method == "DeleteProjectHook" {
			sawDeleteHook = true
		}
	}
	assert.True(t, sawDeleteHook, "deleting the gitlab connection must deregister its linked projects' webhooks")
}

func TestHandleListAvailableGitlabProjects_ReturnsGitlabProjects(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Projects:          []gitlab.Project{{ID: 1, Name: "demo", PathWithNamespace: "group/demo"}},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+projectID+"/gitlab-connection/available-projects?search=demo", nil, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Projects []map[string]any `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Projects, 1)
	assert.Equal(t, "demo", body.Projects[0]["name"])
}

func TestHandleListLinkedGitlabProjectMembers_ReturnsGitlabMembers(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
		Members:           []gitlab.User{{ID: 7, Username: "octocat", Name: "The Octocat"}},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)
	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+linkID+"/members", nil, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Members []map[string]any `json:"members"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Members, 1)
	assert.Equal(t, "octocat", body.Members[0]["username"])
}

func TestHandleListLinkedGitlabProjectMembers_ForeignLinkGets404(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+uuid.NewString()+"/members", nil, token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListLinkedGitlabProjectLabels_ReturnsGitlabLabels(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo"},
		Labels:            []gitlab.Label{{Name: "bug", Color: "#ff0000"}},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	setUpConnectedProject(t, s, token, projectID)
	createRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	linkID := created["id"].(string)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+linkID+"/labels", nil, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Labels []map[string]any `json:"labels"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Labels, 1)
	assert.Equal(t, "bug", body.Labels[0]["name"])
}

func TestHandleListLinkedGitlabProjectLabels_ForeignLinkGets404(t *testing.T) {
	fake := &gitlab.FakeClient{AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"}}
	s, q := newTestServerWithGitlabClient(t, fake)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+uuid.NewString()+"/labels", nil, token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
