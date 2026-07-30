package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCreateTaskDependency(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	design := q.SeedTask(p.ID, ownerID, "Design")
	build := q.SeedTask(p.ID, ownerID, "Build")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/task-dependencies",
		createTaskDependencyRequest{PredecessorTaskID: design.ID, SuccessorTaskID: build.ID}, token)
	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, design.ID.String(), body["predecessorTaskId"])
	assert.Equal(t, build.ID.String(), body["successorTaskId"])
}

func TestHandleCreateTaskDependency_RejectsCycle(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	design := q.SeedTask(p.ID, ownerID, "Design")
	build := q.SeedTask(p.ID, ownerID, "Build")

	require.Equal(t, http.StatusCreated, doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/task-dependencies",
		createTaskDependencyRequest{PredecessorTaskID: design.ID, SuccessorTaskID: build.ID}, token).Code)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/task-dependencies",
		createTaskDependencyRequest{PredecessorTaskID: build.ID, SuccessorTaskID: design.ID}, token)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleCreateTaskDependency_RejectsSelfDependency(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	design := q.SeedTask(p.ID, ownerID, "Design")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/task-dependencies",
		createTaskDependencyRequest{PredecessorTaskID: design.ID, SuccessorTaskID: design.ID}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreateTaskDependency_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	design := q.SeedTask(p.ID, owner.ID, "Design")
	build := q.SeedTask(p.ID, owner.ID, "Build")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/task-dependencies",
		createTaskDependencyRequest{PredecessorTaskID: design.ID, SuccessorTaskID: build.ID}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListTaskDependencies(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	design := q.SeedTask(p.ID, ownerID, "Design")
	build := q.SeedTask(p.ID, ownerID, "Build")
	q.SeedTaskDependency(design.ID, build.ID)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/task-dependencies", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, design.ID.String(), body[0]["predecessorTaskId"])
}

func TestHandleDeleteTaskDependency(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	design := q.SeedTask(p.ID, ownerID, "Design")
	build := q.SeedTask(p.ID, ownerID, "Build")
	d := q.SeedTaskDependency(design.ID, build.ID)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/task-dependencies/"+d.ID.String(), nil, token)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/task-dependencies", nil, token)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &body))
	assert.Empty(t, body)
}

func TestHandleDeleteTaskDependency_ForeignDependencyGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	design := q.SeedTask(p.ID, owner.ID, "Design")
	build := q.SeedTask(p.ID, owner.ID, "Build")
	d := q.SeedTaskDependency(design.ID, build.ID)

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodDelete, "/api/v1/task-dependencies/"+d.ID.String(), nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleDeleteTaskDependency_MissingDependencyGets404(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/task-dependencies/"+uuid.New().String(), nil, token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
