package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setUpLinkedProject seeds a connected project and links a GitLab project to
// it, returning the new link's ID.
func setUpLinkedProject(t *testing.T, s *Server, token, projectID string) string {
	t.Helper()
	setUpConnectedProject(t, s, token, projectID)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+projectID+"/linked-gitlab-projects",
		createLinkedGitlabProjectRequest{GitlabProjectID: 42, SyncScope: "all"}, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	return created["id"].(string)
}

// completeAutoImportRun completes linkID's auto-enqueued project.import run
// (there is no worker polling in these tests, so it would otherwise stay
// 'running' forever and block every manual resync with 409 — see
// TestHandleCreateSyncRun_AlreadyRunningGets409, which relies on exactly
// that).
func completeAutoImportRun(t *testing.T, q *dbtest.FakeQuerier, linkID string) {
	t.Helper()
	id, err := uuid.Parse(linkID)
	require.NoError(t, err)
	runs, err := q.ListGitlabSyncRunsByLinkedGitlabProjectID(context.Background(), id)
	require.NoError(t, err)
	for _, r := range runs {
		if r.Status == "running" {
			_, err := q.CompleteGitlabSyncRun(context.Background(), db.CompleteGitlabSyncRunParams{ID: r.ID})
			require.NoError(t, err)
		}
	}
}

func TestHandleCreateSyncRun_StartsAResync(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	linkID := setUpLinkedProject(t, s, token, projectID)
	completeAutoImportRun(t, q, linkID)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+linkID+"/sync-runs",
		createSyncRunRequest{Full: true}, token)

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "running", body["status"])
	assert.Equal(t, "manual_resync", body["kind"])
}

// TestHandleCreateSyncRun_AlreadyRunningGets409 covers issue #25's
// acceptance criterion that a sync run already in progress for a linked
// project rejects a second one with 409.
func TestHandleCreateSyncRun_AlreadyRunningGets409(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	linkID := setUpLinkedProject(t, s, token, projectID)

	// Linking the project already auto-enqueued a project.import run, which
	// stays 'running' since no worker is polling in this test — so the
	// resync attempted here must already collide with it.
	rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+linkID+"/sync-runs", nil, token)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestHandleCreateSyncRun_ForeignLinkGets404(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	linkID := setUpLinkedProject(t, s, token, projectID)

	_, strangerToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+linkID+"/sync-runs", nil, strangerToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListSyncRuns_ReturnsHistoryNewestFirst(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	linkID := setUpLinkedProject(t, s, token, projectID)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+linkID+"/sync-runs", nil, token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var runs []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &runs))
	require.Len(t, runs, 1, "linking a project auto-enqueues its initial import run")
	assert.Equal(t, "initial_import", runs[0]["kind"])
}

func TestHandleListSyncRuns_ForeignLinkGets404(t *testing.T) {
	fake := &gitlab.FakeClient{
		AuthenticatedUser: &gitlab.User{ID: 7, Username: "octocat"},
		Project:           &gitlab.Project{ID: 42, Name: "demo", PathWithNamespace: "group/demo"},
	}
	s, q := newTestServerWithGitlabClient(t, fake)
	ownerID, token := loginSession(t, s, q)
	projectID := q.SeedProject(ownerID, "Alpha").ID.String()
	linkID := setUpLinkedProject(t, s, token, projectID)

	_, strangerToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+linkID+"/sync-runs", nil, strangerToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
