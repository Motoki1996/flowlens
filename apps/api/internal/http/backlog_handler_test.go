package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/database/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListBacklogs_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleListBacklogs_ScopesToProjectAndOwner(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	otherProject := q.SeedProject(ownerID, "Beta")
	q.SeedBacklog(p.ID, "Sprint 1")
	q.SeedBacklog(p.ID, "Sprint 2")
	q.SeedBacklog(otherProject.ID, "Unrelated")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs", nil, ownerToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body, 2)
}

// Task counts are computed server-side (issue #144) so the web app's Backlog
// collection screen never has to fetch every task itself.
func TestHandleListBacklogs_IncludesTaskCounts(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	q.SeedTaskInBacklog(p.ID, b.ID, ownerID, "Open task")
	closed := q.SeedTaskInBacklog(p.ID, b.ID, ownerID, "Closed task")
	_, err := q.CloseTaskForOwner(context.Background(), db.CloseTaskForOwnerParams{ID: closed.ID, OwnerUserID: ownerID})
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs", nil, ownerToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, float64(2), body[0]["taskCount"])
	assert.Equal(t, float64(1), body[0]["closedTaskCount"])
}

func TestHandleListBacklogs_RejectsInvalidPriorityQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?priority=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListBacklogs_RejectsInvalidProgressQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?progress=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListBacklogs_FiltersAndSortsByProgressQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Done", Progress: "done"}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Not started"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?progress=done", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var filtered []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &filtered))
	require.Len(t, filtered, 1)
	assert.Equal(t, "Done", filtered[0]["name"])

	// Progress sorts not_started first, the reverse of priority's ranking.
	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?sort=progress", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var sorted []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sorted))
	require.Len(t, sorted, 2)
	assert.Equal(t, "Not started", sorted[0]["name"])
	assert.Equal(t, "Done", sorted[1]["name"])
}

func TestHandleListBacklogs_RejectsInvalidSortQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?sort=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListBacklogs_FiltersAndSortsByPriorityQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Low", Priority: "low"}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Urgent", Priority: "urgent"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?priority=urgent", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var filtered []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &filtered))
	require.Len(t, filtered, 1)
	assert.Equal(t, "Urgent", filtered[0]["name"])

	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs?sort=priority", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var sorted []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sorted))
	require.Len(t, sorted, 2)
	assert.Equal(t, "Urgent", sorted[0]["name"])
	assert.Equal(t, "Low", sorted[1]["name"])
}

func TestHandleListBacklogs_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleCreateBacklog_ViewerRoleGets403 covers issue #99: a viewer can
// read the project's backlogs but gets 403, not 404, trying to create one.
func TestHandleCreateBacklog_ViewerRoleGets403(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	viewerID, viewerToken := loginSession(t, s, q)
	q.SeedProjectMember(p.ID, viewerID, "viewer")

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/backlogs", nil, viewerToken)
	require.Equal(t, http.StatusOK, listRec.Code)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1"}, viewerToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleCreateBacklog(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1", Description: "desc"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Sprint 1", body["name"])
	assert.Equal(t, "desc", body["description"])
	assert.Equal(t, float64(0), body["position"])

	id, ok := body["id"].(string)
	require.True(t, ok)
	_, err := uuid.Parse(id)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/backlogs/"+id, nil, token).Code)
}

func TestHandleCreateBacklog_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1"}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCreateBacklog_RejectsInvalidName(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "   "}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreateBacklog_DefaultsPriorityToMedium(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "medium", body["priority"])
	assert.Equal(t, "not_started", body["progress"])
}

func TestHandleCreateBacklog_RejectsInvalidPriority(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1", Priority: "critical"}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreateBacklog_StoresBaseBranch(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1", BaseBranch: "main"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "main", body["baseBranch"])
}

func TestHandleCreateBacklog_RejectsInvalidBaseBranch(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		createBacklogRequest{Name: "Sprint 1", BaseBranch: "bad branch"}, token)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	errBody, _ := body["error"].(map[string]any)
	assert.Equal(t, "invalid_base_branch", errBody["code"])
}

func TestHandleUpdateBacklog_UpdatesBaseBranch(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+b.ID.String(),
		map[string]any{"name": "Sprint 1", "baseBranch": "develop"}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "develop", body["baseBranch"])
}

func TestHandleGetBacklog(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedBacklog(p.ID, "Sprint 1").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		token    string
		wantCode int
	}{
		{"owner can view", "/api/v1/backlogs/" + id, ownerToken, http.StatusOK},
		{"other user gets 404, not 403", "/api/v1/backlogs/" + id, otherToken, http.StatusNotFound},
		{"unknown id gets 404", "/api/v1/backlogs/" + uuid.New().String(), ownerToken, http.StatusNotFound},
		{"malformed id gets 404", "/api/v1/backlogs/not-a-uuid", ownerToken, http.StatusNotFound},
		{"no auth gets 401", "/api/v1/backlogs/" + id, "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodGet, tt.path, nil, tt.token)
			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestHandleUpdateBacklog(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedBacklog(p.ID, "Sprint 1").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	t.Run("other user gets 404 and does not modify the backlog", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id, map[string]any{"name": "Hijacked"}, otherToken)
		require.Equal(t, http.StatusNotFound, rec.Code)

		rec = doRequest(t, s, http.MethodGet, "/api/v1/backlogs/"+id, nil, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Sprint 1", body["name"])
	})

	t.Run("owner can update, including position", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
			map[string]any{"name": "Renamed", "description": "new", "position": 3}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Renamed", body["name"])
		assert.Equal(t, "new", body["description"])
		assert.Equal(t, float64(3), body["position"])
	})

	t.Run("owner can change progress; invalid progress is rejected", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
			map[string]any{"name": "Sprint 1", "progress": "in_progress"}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "in_progress", body["progress"])

		rec = doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
			map[string]any{"name": "Sprint 1", "progress": "nearly-done"}, ownerToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("owner can change priority; invalid priority is rejected", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
			map[string]any{"name": "Sprint 1", "priority": "urgent"}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "urgent", body["priority"])

		rec = doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
			map[string]any{"name": "Sprint 1", "priority": "critical"}, ownerToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// The Backlog timeline reads startDate/dueOn off this endpoint, and the web
// rename form PATCHes without them — so "absent" has to mean "unchanged" while
// an explicit null still clears.
func TestHandleUpdateBacklog_Schedule(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedBacklog(p.ID, "Sprint 1").ID.String()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
		map[string]any{"name": "Sprint 1", "startDate": "2026-08-01T00:00:00Z", "dueOn": "2026-08-31T00:00:00Z"}, token)
	require.Equal(t, http.StatusOK, rec.Code)

	tests := []struct {
		name          string
		body          map[string]any
		wantStartDate any
		wantDueOn     any
	}{
		{
			name:          "absent dates are left alone",
			body:          map[string]any{"name": "Renamed"},
			wantStartDate: "2026-08-01T00:00:00Z",
			wantDueOn:     "2026-08-31T00:00:00Z",
		},
		{
			name:          "explicit null clears only that date",
			body:          map[string]any{"name": "Renamed", "startDate": nil},
			wantStartDate: nil,
			wantDueOn:     "2026-08-31T00:00:00Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id, tt.body, token)
			require.Equal(t, http.StatusOK, rec.Code)
			var got map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			assert.Equal(t, tt.wantStartDate, got["startDate"])
			assert.Equal(t, tt.wantDueOn, got["dueOn"])
		})
	}
}

// defaultLinkedGitlabProjectId (issue #180) is a partial-update field like the
// dates: absent keeps it, an explicit null falls the backlog back to the
// project's default link.
func TestHandleUpdateBacklog_DefaultLinkedGitlabProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted"))
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	id := q.SeedBacklog(p.ID, "Sprint 1").ID.String()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
		map[string]any{"name": "Sprint 1", "defaultLinkedGitlabProjectId": link.ID.String()}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, link.ID.String(), got["defaultLinkedGitlabProjectId"])

	rec = doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id, map[string]any{"name": "Renamed"}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, link.ID.String(), got["defaultLinkedGitlabProjectId"], "an absent field must not reset the link")

	rec = doRequest(t, s, http.MethodPatch, "/api/v1/backlogs/"+id,
		map[string]any{"name": "Renamed", "defaultLinkedGitlabProjectId": nil}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Nil(t, got["defaultLinkedGitlabProjectId"])
}

func TestHandleCreateBacklog_RejectsLinkedGitlabProjectOutsideProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	other := q.SeedProject(ownerID, "Beta")
	conn := q.SeedGitlabConnection(other.ID, []byte("encrypted"))
	foreign, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    200,
		PathWithNamespace:  "group/other",
		Name:               "other",
		WebUrl:             "https://gitlab.example.com/group/other",
		SyncScope:          "all",
	})
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		map[string]any{"name": "Sprint 1", "defaultLinkedGitlabProjectId": foreign.ID.String()}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreateBacklog_RejectsStartAfterDue(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		map[string]any{"name": "Sprint 1", "startDate": "2026-09-01T00:00:00Z", "dueOn": "2026-08-31T00:00:00Z"}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDeleteBacklog(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedBacklog(p.ID, "Sprint 1").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	// A non-owner gets 404 and the backlog survives.
	require.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/backlogs/"+id, nil, otherToken).Code)
	require.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/backlogs/"+id, nil, ownerToken).Code)

	require.Equal(t, http.StatusNoContent, doRequest(t, s, http.MethodDelete, "/api/v1/backlogs/"+id, nil, ownerToken).Code)
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/backlogs/"+id, nil, ownerToken).Code)

	// Deleting twice is reported as "not found", not as success.
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/backlogs/"+id, nil, ownerToken).Code)
}

func TestHandleReorderBacklogs(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	first := q.SeedBacklog(p.ID, "First")
	second := q.SeedBacklog(p.ID, "Second")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+p.ID.String()+"/backlogs/order",
		reorderBacklogsRequest{BacklogIDs: []uuid.UUID{second.ID, first.ID}}, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 2)
	assert.Equal(t, second.ID.String(), body[0]["id"])
	assert.Equal(t, first.ID.String(), body[1]["id"])
}

func TestHandleReorderBacklogs_RejectsMismatchedBacklogIDs(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	q.SeedBacklog(p.ID, "Only backlog")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+p.ID.String()+"/backlogs/order",
		reorderBacklogsRequest{BacklogIDs: []uuid.UUID{uuid.New()}}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleReorderBacklogs_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+p.ID.String()+"/backlogs/order",
		reorderBacklogsRequest{}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
