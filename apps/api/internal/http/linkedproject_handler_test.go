package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/gitlab"
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
