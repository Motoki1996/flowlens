package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetProjectMetrics_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/metrics", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGetProjectMetrics_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/metrics", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetProjectMetrics_NoData(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/metrics", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	openToFirstReview := body["openToFirstReview"].(map[string]any)
	assert.Equal(t, float64(0), openToFirstReview["count"])
	assert.Nil(t, openToFirstReview["medianHours"])
	assert.Equal(t, float64(0), body["throughput"])
}

func TestHandleGetProjectMetrics_CountsMergeRequestsInRange(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	repo := seedRepository(t, q, p)
	q.SeedMergeRequest(repo.ID, 1, 1, "Merged one", "merged")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/metrics", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, float64(1), body["throughput"])
}

func TestHandleGetProjectMetrics_RejectsInvalidFromQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/metrics?from=not-a-date", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
