package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/flowlens/api/internal/syncjob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListFailedSyncJobs_ReturnsFailedJobsForOwnedProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	failed := q.SeedSyncJob(p.ID, "issue.update", "failed", time.Now())
	q.SeedSyncJob(p.ID, "issue.update", "succeeded", time.Now())

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/sync-jobs?status=failed", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var jobs []syncjob.Job
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &jobs))
	require.Len(t, jobs, 1)
	assert.Equal(t, failed.ID, jobs[0].ID)
	assert.Equal(t, "failed", jobs[0].Status)
}

func TestHandleListFailedSyncJobs_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	q.SeedSyncJob(p.ID, "issue.update", "failed", time.Now())

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/sync-jobs", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleRetrySyncJob_FailedJob_ResetsAndDisappearsFromList(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	job := q.SeedSyncJob(p.ID, "issue.update", "failed", time.Now())

	rec := doRequest(t, s, http.MethodPost, "/api/v1/sync-jobs/"+job.ID.String()+"/retry", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var got syncjob.Job
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "pending", got.Status)

	listRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/sync-jobs", nil, token)
	require.Equal(t, http.StatusOK, listRec.Code)
	var jobs []syncjob.Job
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &jobs))
	assert.Empty(t, jobs, "a retried job must no longer show up as failed")
}

func TestHandleRetrySyncJob_NotFailed_Returns409(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	job := q.SeedSyncJob(p.ID, "issue.update", "pending", time.Now())

	rec := doRequest(t, s, http.MethodPost, "/api/v1/sync-jobs/"+job.ID.String()+"/retry", nil, token)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleRetrySyncJob_ForeignJobGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	job := q.SeedSyncJob(p.ID, "issue.update", "failed", time.Now())

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/sync-jobs/"+job.ID.String()+"/retry", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
