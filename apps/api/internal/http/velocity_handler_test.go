package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetProjectVelocity_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGetProjectVelocity_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetProjectVelocity_DefaultsIntervalToWeek(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "week", body["interval"])
	assert.Equal(t, float64(0), body["openTaskCount"])
}

func TestHandleGetProjectVelocity_CountsCompletedTasks(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	completed := time.Now().Add(-time.Hour)
	task := q.SeedTaskWithCreatedAt(p.ID, ownerID, "Task", completed.Add(-48*time.Hour))
	q.SeedTaskProgressEventWithActor(task.ID, "in_progress", "done", completed, "agent")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	periods := body["periods"].([]any)
	require.NotEmpty(t, periods)
	last := periods[len(periods)-1].(map[string]any)
	assert.Equal(t, float64(1), last["completed"])
	assert.Equal(t, float64(1), last["completedByAgent"])
}

func TestHandleGetProjectVelocity_RejectsInvalidFromQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity?from=not-a-date", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetProjectVelocity_RejectsInvalidInterval(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity?interval=decade", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetProjectVelocity_AcceptsExplicitInterval(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/velocity?interval=month", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "month", body["interval"])
}
