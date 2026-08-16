package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetProjectFlowMetrics_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/flow-metrics", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGetProjectFlowMetrics_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/flow-metrics", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetProjectFlowMetrics_NoData(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/flow-metrics", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	waitingToStart := body["waitingToStart"].(map[string]any)
	assert.Equal(t, float64(0), waitingToStart["count"])
	assert.Nil(t, waitingToStart["medianHours"])
}

func TestHandleGetProjectFlowMetrics_CountsTasksInRange(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	created := time.Now().Add(-3 * time.Hour)
	task := q.SeedTaskWithCreatedAt(p.ID, ownerID, "Task", created)
	q.SeedTaskProgressEvent(task.ID, "not_started", "in_progress", created.Add(time.Hour))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/flow-metrics", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	waitingToStart := body["waitingToStart"].(map[string]any)
	assert.Equal(t, float64(1), waitingToStart["count"])
}

func TestHandleGetProjectFlowMetrics_RejectsInvalidFromQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/flow-metrics?from=not-a-date", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
