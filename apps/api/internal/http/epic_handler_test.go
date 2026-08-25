package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListEpics_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleListEpics_ScopesToProjectAndOwner(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	other := q.SeedProject(ownerID, "Beta")
	q.SeedEpic(p.ID, uuid.Nil, "Screens")
	q.SeedEpic(p.ID, uuid.Nil, "API")
	q.SeedEpic(other.ID, uuid.Nil, "Unrelated")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body, 2)
}

func TestHandleListEpics_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, intruderToken := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListEpics_FiltersByBacklog(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	q.SeedEpic(p.ID, b.ID, "Screens")
	q.SeedEpic(p.ID, uuid.Nil, "Loose ends")

	filed := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics?backlog_id="+b.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, filed.Code)
	var inBacklog []map[string]any
	require.NoError(t, json.Unmarshal(filed.Body.Bytes(), &inBacklog))
	require.Len(t, inBacklog, 1)
	assert.Equal(t, "Screens", inBacklog[0]["name"])

	unfiled := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics?backlog_id=unassigned", nil, token)
	require.Equal(t, http.StatusOK, unfiled.Code)
	var loose []map[string]any
	require.NoError(t, json.Unmarshal(unfiled.Body.Bytes(), &loose))
	require.Len(t, loose, 1)
	assert.Equal(t, "Loose ends", loose[0]["name"])
}

func TestHandleListEpics_RejectsInvalidQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	for _, query := range []string{"?priority=bogus", "?progress=bogus", "?sort=bogus", "?backlog_id=nope"} {
		rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics"+query, nil, token)
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
}

func TestHandleCreateEpic(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/epics",
		createEpicRequest{Name: "Screens", Description: "desc", BacklogID: &b.ID, BaseBranch: "release/2.4"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Screens", body["name"])
	assert.Equal(t, "release/2.4", body["baseBranch"])
	assert.Equal(t, b.ID.String(), body["backlogId"])
	assert.Equal(t, "medium", body["priority"])
	assert.Equal(t, "not_started", body["progress"])

	id, ok := body["id"].(string)
	require.True(t, ok)
	assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/epics/"+id, nil, token).Code)
}

func TestHandleCreateEpic_ViewerRoleGets403(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	viewerID, viewerToken := loginSession(t, s, q)
	q.SeedProjectMember(p.ID, viewerID, "viewer")

	require.Equal(t, http.StatusOK,
		doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics", nil, viewerToken).Code)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/epics",
		createEpicRequest{Name: "Screens"}, viewerToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// Each domain sentinel has to reach the client as its own 400 code — that is
// what the web form's inline error messages key on.
func TestHandleCreateEpic_MapsValidationErrors(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	other := q.SeedProject(ownerID, "Beta")
	foreignBacklog := q.SeedBacklog(other.ID, "Their sprint")
	stranger := q.SeedUser("hubot", "hubot@example.com").ID

	tests := []struct {
		name     string
		req      createEpicRequest
		wantCode string
	}{
		{"blank name", createEpicRequest{Name: "  "}, "invalid_name"},
		{"unknown priority", createEpicRequest{Name: "Screens", Priority: "critical"}, "invalid_priority"},
		{"unknown progress", createEpicRequest{Name: "Screens", Progress: "started"}, "invalid_progress"},
		{"bad branch", createEpicRequest{Name: "Screens", BaseBranch: "no spaces"}, "invalid_base_branch"},
		{"foreign backlog", createEpicRequest{Name: "Screens", BacklogID: &foreignBacklog.ID}, "invalid_backlog"},
		{"non-member assignee", createEpicRequest{Name: "Screens", AssigneeUserID: &stranger}, "invalid_assignee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/epics", tt.req, token)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantCode, body.Error.Code)
		})
	}
}

func TestHandleUpdateEpic(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	e := q.SeedEpic(p.ID, b.ID, "Screens")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String(),
		map[string]any{"name": "Screens v2", "baseBranch": "develop"}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Screens v2", body["name"])
	assert.Equal(t, "develop", body["baseBranch"])
	// An absent key is left alone, not cleared.
	assert.Equal(t, b.ID.String(), body["backlogId"])
}

func TestHandleUpdateEpic_ForeignEpicGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	e := q.SeedEpic(p.ID, uuid.Nil, "Screens")
	_, intruderToken := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String(), map[string]any{"name": "Mine now"}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleDeleteEpic(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	e := q.SeedEpic(p.ID, b.ID, "Screens")
	tsk := q.SeedTaskInBacklog(p.ID, b.ID, ownerID, "Build it")
	q.SeedTaskEpic(tsk.ID, e.ID)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/epics/"+e.ID.String(), nil, token)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/epics/"+e.ID.String(), nil, token).Code)

	// The task survives, back in its backlog.
	assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String(), nil, token).Code)
}

func TestHandleReorderEpics(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	first := q.SeedEpic(p.ID, uuid.Nil, "First")
	second := q.SeedEpic(p.ID, uuid.Nil, "Second")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+p.ID.String()+"/epics/order",
		reorderEpicsRequest{EpicIDs: []uuid.UUID{second.ID, first.ID}}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 2)
	assert.Equal(t, "Second", body[0]["name"])

	mismatch := doRequest(t, s, http.MethodPatch, "/api/v1/projects/"+p.ID.String()+"/epics/order",
		reorderEpicsRequest{EpicIDs: []uuid.UUID{second.ID}}, token)
	assert.Equal(t, http.StatusBadRequest, mismatch.Code)
}

// The epic's own half of the task<->epic relationship: PATCH .../tasks writes
// the whole set, and PATCH /tasks/{id}'s epicId writes one task's side of it.
func TestHandleSetEpicTasks(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	e := q.SeedEpic(p.ID, b.ID, "Screens")
	first := q.SeedTask(p.ID, ownerID, "Build the list screen")
	second := q.SeedTask(p.ID, ownerID, "Build the detail screen")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String()+"/tasks",
		setEpicTasksRequest{TaskIDs: []uuid.UUID{first.ID, second.ID}}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, e.ID.String(), body["id"])

	// Both are in the epic, and in its backlog with it.
	for _, id := range []uuid.UUID{first.ID, second.ID} {
		stored := q.TaskByID(id)
		require.True(t, stored.EpicID.Valid)
		assert.Equal(t, b.ID.String(), uuid.UUID(stored.BacklogID.Bytes).String())
	}

	// The set is declarative: what it no longer names drops out.
	rec = doRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String()+"/tasks",
		setEpicTasksRequest{TaskIDs: []uuid.UUID{first.ID}}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, q.TaskByID(second.ID).EpicID.Valid)
}

func TestHandleSetEpicTasks_RejectsForeignTask(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	other := q.SeedProject(ownerID, "Beta")
	e := q.SeedEpic(p.ID, uuid.Nil, "Screens")
	foreign := q.SeedTask(other.ID, ownerID, "Theirs")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String()+"/tasks",
		setEpicTasksRequest{TaskIDs: []uuid.UUID{foreign.ID}}, token)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "invalid_tasks", body.Error.Code)
	assert.False(t, q.TaskByID(foreign.ID).EpicID.Valid)
}

func TestHandleSetEpicTasks_RequiresWriteScope(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	e := q.SeedEpic(p.ID, uuid.Nil, "Screens")
	tsk := q.SeedTask(p.ID, owner.ID, "Ours")

	_, readToken, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)
	_, writeToken, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "Agent", []string{"write"}, nil)
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden,
		doBearerRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String()+"/tasks",
			setEpicTasksRequest{TaskIDs: []uuid.UUID{tsk.ID}}, readToken).Code)
	assert.Equal(t, http.StatusOK,
		doBearerRequest(t, s, http.MethodPatch, "/api/v1/epics/"+e.ID.String()+"/tasks",
			setEpicTasksRequest{TaskIDs: []uuid.UUID{tsk.ID}}, writeToken).Code)
}

// An agent breaking a backlog down into epics reaches these routes with a
// bearer token, so the scope and project-boundary checks matter as much here
// as on the task routes.
func TestHandleEpics_BearerAuth(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	other := q.SeedProject(owner.ID, "Beta")
	e := q.SeedEpic(p.ID, uuid.Nil, "Screens")

	_, readToken, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{"read"}, nil)
	require.NoError(t, err)
	_, writeToken, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "Agent", []string{"write"}, nil)
	require.NoError(t, err)
	_, foreignToken, err := s.apiTokens.Create(context.Background(), owner.ID, other.ID, "Other bot", []string{"write"}, nil)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK,
		doBearerRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics", nil, readToken).Code)
	assert.Equal(t, http.StatusOK,
		doBearerRequest(t, s, http.MethodGet, "/api/v1/epics/"+e.ID.String(), nil, readToken).Code)

	// read cannot write.
	assert.Equal(t, http.StatusForbidden,
		doBearerRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/epics",
			createEpicRequest{Name: "API"}, readToken).Code)

	assert.Equal(t, http.StatusCreated,
		doBearerRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/epics",
			createEpicRequest{Name: "API"}, writeToken).Code)

	// A token cannot reach another project's epics, by collection or by ID.
	assert.Equal(t, http.StatusNotFound,
		doBearerRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/epics", nil, foreignToken).Code)
	assert.Equal(t, http.StatusNotFound,
		doBearerRequest(t, s, http.MethodGet, "/api/v1/epics/"+e.ID.String(), nil, foreignToken).Code)
}
