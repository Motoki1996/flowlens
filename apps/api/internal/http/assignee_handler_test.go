package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wire contract for the FlowLens assignee: one representative case per
// branch, with the exhaustive rules covered in internal/task and
// internal/backlog (see docs/testing.md's layering).

func TestHandleUpdateTask_SetsAndClearsAssigneeUserID(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	member := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	taskID := created["id"].(string)

	rec = doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+taskID,
		map[string]any{"assigneeUserId": member.ID.String()}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var updated map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Equal(t, member.ID.String(), updated["assigneeUserId"])
	assert.Equal(t, "hubot", updated["assigneeUsername"])

	rec = doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+taskID,
		map[string]any{"assigneeUserId": nil}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	assert.Nil(t, updated["assigneeUserId"])
	assert.Equal(t, "", updated["assigneeUsername"])
}

func TestHandleUpdateTask_RejectsNonMemberAssignee(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	outsider := q.SeedUser("stranger", "stranger@example.com")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	rec = doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+created["id"].(string),
		map[string]any{"assigneeUserId": outsider.ID.String()}, token)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_assignee")
}

// ?assignee= now takes a UUID, not just "me" — the whole point of the change:
// a lead can see what someone else is carrying.
func TestHandleListTasks_FiltersByAnotherUsersAssignee(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	member := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "Theirs", "assigneeUserId": member.ID.String()}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Nobody's"}, token)

	rec = doRequest(t, s, http.MethodGet,
		"/api/v1/projects/"+p.ID.String()+"/tasks?assignee="+member.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Theirs", body[0]["title"])

	rec = doRequest(t, s, http.MethodGet,
		"/api/v1/projects/"+p.ID.String()+"/tasks?assignee=unassigned", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Nobody's", body[0]["title"])
}

func TestHandleListAllTasks_FiltersByAnotherUsersAssignee(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	member := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "Theirs", "assigneeUserId": member.ID.String()}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Nobody's"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks?assignee="+member.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Theirs", body[0]["title"])
}

func TestHandleUpdateBacklog_SetsAssigneeUserID(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	member := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		map[string]any{"name": "Sprint 1", "assigneeUserId": member.ID.String()}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, member.ID.String(), created["assigneeUserId"])
	assert.Equal(t, "hubot", created["assigneeUsername"])

	rec = doRequest(t, s, http.MethodGet,
		"/api/v1/projects/"+p.ID.String()+"/backlogs?assignee="+member.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "Sprint 1", list[0]["name"])
}

func TestHandleCreateBacklog_RejectsNonMemberAssignee(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	outsider := q.SeedUser("stranger", "stranger@example.com")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/backlogs",
		map[string]any{"name": "Sprint 1", "assigneeUserId": outsider.ID.String()}, token)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_assignee")
}

// ?assignee=me over a bearer token resolves to the token's project owner, not
// to nobody: a token acts *as* that owner everywhere else in the API
// (internal/apitoken, ADR-0009), and this filter is no exception. Pinned
// because the alternative reading — "a token has no 'me', so match nothing" —
// would silently return an empty list to an agent.
func TestHandleListTasks_AssigneeMeOverBearerTokenIsTheProjectOwner(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "Owner's", "assigneeUserId": owner.ID.String()}, raw)
	require.Equal(t, http.StatusCreated, rec.Code)
	doBearerRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "Nobody's"}, raw)

	rec = doBearerRequest(t, s, http.MethodGet,
		"/api/v1/projects/"+p.ID.String()+"/tasks?assignee=me", nil, raw)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Owner's", body[0]["title"])
}
