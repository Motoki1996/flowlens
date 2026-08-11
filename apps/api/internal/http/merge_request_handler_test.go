package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/mrsync"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRepository creates a GitLab connection, linked GitLab project and
// repository for p, mirroring internal/mrsync's own test fixture, so
// merge-request tests have somewhere to seed merge requests into.
func seedRepository(t *testing.T, q *dbtest.FakeQuerier, p db.Project) db.Repository {
	t.Helper()
	conn := q.SeedGitlabConnection(p.ID, nil)
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/project",
		Name:               "project",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)
	repo, err := mrsync.EnsureRepository(context.Background(), q, link)
	require.NoError(t, err)
	return repo
}

func TestHandleListMergeRequests_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleListMergeRequests_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListMergeRequests_FiltersByStateQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	repo := seedRepository(t, q, p)
	q.SeedMergeRequest(repo.ID, 1, 1, "Opened one", "opened")
	q.SeedMergeRequest(repo.ID, 2, 2, "Merged one", "merged")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests?state=merged", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Merged one", body[0]["title"])
}

func TestHandleListMergeRequests_FiltersByTaskIDQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	repo := seedRepository(t, q, p)
	task := q.SeedTask(p.ID, ownerID, "Fix the bug")
	linked := q.SeedMergeRequest(repo.ID, 1, 1, "Linked", "opened")
	_, err := q.UpdateMergeRequestTaskID(context.Background(), db.UpdateMergeRequestTaskIDParams{
		ID: linked.ID, TaskID: pgtype.UUID{Bytes: task.ID, Valid: true},
	})
	require.NoError(t, err)
	q.SeedMergeRequest(repo.ID, 2, 2, "Unlinked", "opened")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests?taskId="+task.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Linked", body[0]["title"])
}

func TestHandleListMergeRequests_RejectsInvalidStateQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests?state=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListMergeRequests_RejectsInvalidTaskIDQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests?taskId=not-a-uuid", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListMergeRequests_RejectsInvalidSinceQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests?since=not-a-date", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListMergeRequests_SortsByUpdated(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	repo := seedRepository(t, q, p)
	older := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := q.CreateMergeRequest(context.Background(), db.CreateMergeRequestParams{
		RepositoryID: repo.ID, GitlabMergeRequestID: 1, Number: 1, Title: "Updated last", State: "opened",
		GitlabUpdatedAt: pgtype.Timestamptz{Time: older, Valid: true},
	})
	require.NoError(t, err)
	_, err = q.CreateMergeRequest(context.Background(), db.CreateMergeRequestParams{
		RepositoryID: repo.ID, GitlabMergeRequestID: 2, Number: 2, Title: "Updated first", State: "opened",
		GitlabUpdatedAt: pgtype.Timestamptz{Time: newer, Valid: true},
	})
	require.NoError(t, err)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/merge-requests?sort=updated", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 2)
	assert.Equal(t, "Updated first", body[0]["title"])
}

func TestHandleGetMergeRequest(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	repo := seedRepository(t, q, p)
	mr := q.SeedMergeRequest(repo.ID, 1, 1, "Mine", "opened")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/merge-requests/"+mr.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Mine", body["title"])
}

func TestHandleGetMergeRequest_ForeignMRGets404(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, _ := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	repo := seedRepository(t, q, p)
	mr := q.SeedMergeRequest(repo.ID, 1, 1, "Mine", "opened")

	_, intruderToken := loginSessionAs(t, s, q, "mallory", "mallory@example.com")
	rec := doRequest(t, s, http.MethodGet, "/api/v1/merge-requests/"+mr.ID.String(), nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetMergeRequest_UnknownIDGets404(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/merge-requests/"+uuid.New().String(), nil, token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
