package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListTasks_NoCookie(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleListTasks_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListTasks_FiltersByBacklogIDQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string // task titles expected in the response, any order
	}{
		{"no filter returns everything", "", []string{"In backlog", "Unfiled"}},
		{"backlog_id=unassigned returns only unfiled tasks", "?backlog_id=unassigned", []string{"Unfiled"}},
		{"backlog_id=<id> scopes to one backlog", "?backlog_id={backlogID}", []string{"In backlog"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			ownerID, token := loginSession(t, s, q)
			p := q.SeedProject(ownerID, "Alpha")
			b := q.SeedBacklog(p.ID, "Sprint 1")
			q.SeedTaskInBacklog(p.ID, b.ID, ownerID, "In backlog")
			q.SeedTask(p.ID, ownerID, "Unfiled")

			query := tt.query
			if query == "?backlog_id={backlogID}" {
				query = "?backlog_id=" + b.ID.String()
			}

			rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks"+query, nil, token)
			require.Equal(t, http.StatusOK, rec.Code)

			var body []map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			titles := make([]string, len(body))
			for i, task := range body {
				titles[i] = task["title"].(string)
			}
			assert.ElementsMatch(t, tt.want, titles)
		})
	}
}

func TestHandleListTasks_RejectsInvalidBacklogIDQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?backlog_id=not-a-uuid", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListTasks_RejectsInvalidStatusQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?status=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListTasks_RejectsInvalidPriorityQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?priority=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListTasks_RejectsInvalidProgressQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?progress=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListTasks_FiltersAndSortsByProgressQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Done", Progress: "done"}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Not started"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?progress=done", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var filtered []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &filtered))
	require.Len(t, filtered, 1)
	assert.Equal(t, "Done", filtered[0]["title"])

	// Progress sorts not_started first, the reverse of priority's ranking.
	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?sort=progress", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var sorted []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sorted))
	require.Len(t, sorted, 2)
	assert.Equal(t, "Not started", sorted[0]["title"])
	assert.Equal(t, "Done", sorted[1]["title"])
}

func TestHandleListTasks_RejectsInvalidSortQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?sort=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListTasks_FiltersAndSortsByPriorityQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Low", Priority: "low"}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Urgent", Priority: "urgent"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?priority=urgent", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var filtered []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &filtered))
	require.Len(t, filtered, 1)
	assert.Equal(t, "Urgent", filtered[0]["title"])

	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?sort=priority", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var sorted []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sorted))
	require.Len(t, sorted, 2)
	assert.Equal(t, "Urgent", sorted[0]["title"])
	assert.Equal(t, "Low", sorted[1]["title"])
}

// TestHandleListTasks_FiltersByQQuery covers issue #106's wire contract:
// ?q= narrows to tasks whose title or description matches. Which rows
// search_vector actually matches is the domain/integration layer's case.
func TestHandleListTasks_FiltersByQQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix login bug"}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Unrelated task"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?q=login", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var filtered []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &filtered))
	require.Len(t, filtered, 1)
	assert.Equal(t, "Fix login bug", filtered[0]["title"])
}

// The wire contract for ?sort=: the project-scoped list takes the same three
// values as GET /api/v1/tasks, and rejects anything else. Which order each
// value produces is the domain layer's case (TestService_List_*).
func TestHandleListTasks_SortQueryAcceptsTheCrossProjectValues(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Only", DueOn: nil}, token)

	tests := []struct {
		sort     string
		wantCode int
	}{
		{"priority", http.StatusOK},
		{"dueOn", http.StatusOK},
		{"updatedAt", http.StatusOK},
		{"title", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.sort, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?sort="+tt.sort, nil, token)
			assert.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
		})
	}
}

// TestHandleListTasks_FiltersByAssigneeMeQuery covers issue #102's completion
// conditions: ?assignee=me returns only tasks assigned to the caller's own
// registered GitLab identity for the project's own connection, and a caller
// with no registered identity gets an empty list rather than an error.
func TestHandleListTasks_FiltersByAssigneeMeQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, nil)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?assignee=me", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "[]", rec.Body.String(), "no identity registered yet must return an empty list, not an error")

	rec = doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Mine", AssigneeGitlabUserID: int64Ptr(42)}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Someone else's", AssigneeGitlabUserID: int64Ptr(99)}, token)

	doRequest(t, s, http.MethodPut, "/api/v1/me/gitlab-identities",
		putGitlabIdentityRequest{GitlabBaseURL: conn.BaseUrl, GitlabUserID: 42, GitlabUsername: "octocat"}, token)

	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?assignee=me", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Mine", body[0]["title"])
}

func TestHandleListTasks_RejectsInvalidAssigneeQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?assignee=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleListAllTasks_NoCookie(t *testing.T) {
	s, _ := newTestServer(t)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestHandleListAllTasks_SpansOwnProjectsAndExcludesOthers is the completion
// condition issue #76 calls out explicitly: the cross-project endpoint must
// gather every task the caller owns and never leak another user's.
func TestHandleListAllTasks_SpansOwnProjectsAndExcludesOthers(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	alpha := q.SeedProject(ownerID, "Alpha")
	beta := q.SeedProject(ownerID, "Beta")
	q.SeedTask(alpha.ID, ownerID, "In alpha")
	q.SeedTask(beta.ID, ownerID, "In beta")

	otherID, _ := loginSession(t, s, q)
	theirs := q.SeedProject(otherID, "Theirs")
	q.SeedTask(theirs.ID, otherID, "Not mine")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	titles := make([]string, len(body))
	projectNames := make([]string, len(body))
	for i, tk := range body {
		titles[i] = tk["title"].(string)
		projectNames[i] = tk["projectName"].(string)
	}
	assert.ElementsMatch(t, []string{"In alpha", "In beta"}, titles)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, projectNames)
}

func TestHandleListAllTasks_FiltersByStatusQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Open task"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Closed task"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+created["id"].(string)+"/close", nil, token)

	rec = doRequest(t, s, http.MethodGet, "/api/v1/tasks?status=open", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Open task", body[0]["title"])
}

func TestHandleListAllTasks_FiltersByQQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix login bug"}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Unrelated task"}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks?q=login", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Fix login bug", body[0]["title"])
}

// TestHandleListAllTasks_FiltersByAssigneeMeQuery is
// TestHandleListTasks_FiltersByAssigneeMeQuery for the cross-project
// collection: it must resolve the identity match per task's own project
// (issue #102), not against a single project-wide connection.
func TestHandleListAllTasks_FiltersByAssigneeMeQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, nil)

	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Mine", AssigneeGitlabUserID: int64Ptr(42)}, token)
	doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Someone else's", AssigneeGitlabUserID: int64Ptr(99)}, token)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks?assignee=me", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "[]", rec.Body.String(), "no identity registered yet must return an empty list, not an error")

	doRequest(t, s, http.MethodPut, "/api/v1/me/gitlab-identities",
		putGitlabIdentityRequest{GitlabBaseURL: conn.BaseUrl, GitlabUserID: 42, GitlabUsername: "octocat"}, token)

	rec = doRequest(t, s, http.MethodGet, "/api/v1/tasks?assignee=me", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	assert.Equal(t, "Mine", body[0]["title"])
}

func TestHandleListAllTasks_RejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"invalid status", "?status=bogus"},
		{"invalid priority", "?priority=bogus"},
		{"invalid progress", "?progress=bogus"},
		{"invalid size", "?size=bogus"},
		{"invalid sort", "?sort=bogus"},
		{"invalid dueBefore", "?dueBefore=not-a-date"},
		{"invalid dueAfter", "?dueAfter=not-a-date"},
		{"invalid startedBefore", "?startedBefore=not-a-date"},
		{"invalid projectId", "?projectId=not-a-uuid"},
		{"invalid assignee", "?assignee=bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			_, token := loginSession(t, s, q)

			rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks"+tt.query, nil, token)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestHandleListAllTasks_LimitCapsResults(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	q.SeedTask(p.ID, ownerID, "One")
	q.SeedTask(p.ID, ownerID, "Two")
	q.SeedTask(p.ID, ownerID, "Three")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks?limit=2", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body, 2)
}

func int64Ptr(v int64) *int64 { return &v }

func TestHandleCreateTask(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug", Description: "desc"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Fix bug", body["title"])
	assert.Equal(t, "desc", body["description"])
	assert.Equal(t, "open", body["status"])
	assert.Nil(t, body["backlogId"])
	assert.Nil(t, body["gitlab"])
	assert.Contains(t, body, "gitlab")

	id, ok := body["id"].(string)
	require.True(t, ok)
	_, err := uuid.Parse(id)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, token).Code)
}

// startDate is app-only (issue #33's Gantt chart): unlike dueOn, it has no
// GitLab counterpart, but it round-trips through the same wire contract.
func TestHandleCreateTask_PersistsStartDate(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug", StartDate: &start}, token)
	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "2026-08-01T00:00:00Z", body["startDate"])
}

func TestHandleCreateTask_RejectsInvalidTitle(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "   "}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreateTask_RejectsInvalidPriority(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug", Priority: "critical"}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// An absent priority on create defaults to "medium", the same as the domain
// layer's own default.
func TestHandleCreateTask_DefaultsPriorityToMedium(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug"}, token)
	require.Equal(t, http.StatusCreated, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "medium", body["priority"])
	assert.Equal(t, "not_started", body["progress"])
}

func TestHandleCreateTask_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug"}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleCreateTask_ViewerRoleGets403 covers issue #99's completion
// criterion: a viewer-role member can read a project but gets 403, not 404,
// on a write — the caller's existence check already succeeded, only the
// role check fails.
func TestHandleCreateTask_ViewerRoleGets403(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	viewerID, viewerToken := loginSession(t, s, q)
	q.SeedProjectMember(p.ID, viewerID, "viewer")

	// The viewer can still read the project.
	getRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String(), nil, viewerToken)
	require.Equal(t, http.StatusOK, getRec.Code)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug"}, viewerToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestHandleCreateTask_NonMemberGets404 confirms a user with no
// project_members row at all still gets 404, never leaking that the project
// exists (issue #99).
func TestHandleCreateTask_NonMemberGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	_, strangerToken := loginSession(t, s, q)

	readRec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String(), nil, strangerToken)
	assert.Equal(t, http.StatusNotFound, readRec.Code)

	writeRec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug"}, strangerToken)
	assert.Equal(t, http.StatusNotFound, writeRec.Code)
}

func TestHandleCreateTask_RejectsBacklogFromAnotherProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	otherProject := q.SeedProject(ownerID, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		createTaskRequest{Title: "Fix bug", BacklogID: &foreignBacklog.ID}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleGetTask(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	tests := []struct {
		name     string
		path     string
		token    string
		wantCode int
	}{
		{"owner can view", "/api/v1/tasks/" + id, ownerToken, http.StatusOK},
		{"other user gets 404, not 403", "/api/v1/tasks/" + id, otherToken, http.StatusNotFound},
		{"unknown id gets 404", "/api/v1/tasks/" + uuid.New().String(), ownerToken, http.StatusNotFound},
		{"malformed id gets 404", "/api/v1/tasks/not-a-uuid", ownerToken, http.StatusNotFound},
		{"no auth gets 401", "/api/v1/tasks/" + id, "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodGet, tt.path, nil, tt.token)
			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestHandleUpdateTask(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	t.Run("other user gets 404 and does not modify the task", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id, map[string]any{"title": "Hijacked"}, otherToken)
		require.Equal(t, http.StatusNotFound, rec.Code)

		rec = doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Fix bug", body["title"])
	})

	t.Run("owner can update title and description", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
			map[string]any{"title": "Renamed", "description": "new"}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Renamed", body["title"])
		assert.Equal(t, "new", body["description"])
	})

	t.Run("owner can change progress; invalid progress is rejected", func(t *testing.T) {
		// Progress travels alone in the body and must not disturb status,
		// which only /close and /reopen move.
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
			map[string]any{"progress": "on_hold"}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "on_hold", body["progress"])
		assert.Equal(t, "open", body["status"])

		rec = doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
			map[string]any{"progress": "nearly-done"}, ownerToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("owner can change priority; invalid priority is rejected", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
			map[string]any{"priority": "urgent"}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "urgent", body["priority"])

		rec = doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
			map[string]any{"priority": "critical"}, ownerToken)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// A progress-changing PATCH over a session cookie is attributed to the
// task_progress_events log as actor_kind "user" (issue #169).
func TestHandleUpdateTask_SessionAuth_ProgressChangeRecordsUserActor(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+tsk.ID.String(),
		map[string]any{"progress": "in_progress"}, token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	events, err := q.ListTaskProgressEventsByTask(context.Background(), tsk.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, task.ActorKindUser, events[0].ActorKind)
	require.True(t, events[0].ActorUserID.Valid)
	assert.Equal(t, ownerID, uuid.UUID(events[0].ActorUserID.Bytes))
}

// The same PATCH over a bearer token is attributed as actor_kind "agent",
// so the two transition sources can be told apart later (issue #169).
func TestHandleUpdateTask_BearerAuth_ProgressChangeRecordsAgentActor(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+tsk.ID.String(),
		map[string]any{"progress": "in_progress"}, raw)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	events, err := q.ListTaskProgressEventsByTask(context.Background(), tsk.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, task.ActorKindAgent, events[0].ActorKind)
	assert.False(t, events[0].ActorUserID.Valid, "an agent-attributed event has no actor user")
}

// PATCH is a partial update at the wire level too: a key absent from the
// JSON body leaves the field alone, and an explicit null clears a nullable
// one. The web edit form relies on this to send only what it shows.
func TestHandleUpdateTask_AppliesOnlyKeysPresentInBody(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
		map[string]any{"title": "Renamed", "description": "original", "dueOn": "2026-09-01T00:00:00Z"}, token)
	require.Equal(t, http.StatusOK, rec.Code)

	tests := []struct {
		name string
		body map[string]any
		want map[string]any
	}{
		{
			name: "title only keeps description and due date",
			body: map[string]any{"title": "Renamed again"},
			want: map[string]any{"title": "Renamed again", "description": "original", "dueOn": "2026-09-01T00:00:00Z"},
		},
		{
			name: "explicit null clears the due date",
			body: map[string]any{"dueOn": nil},
			want: map[string]any{"title": "Renamed again", "description": "original", "dueOn": nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id, tt.body, token)
			require.Equal(t, http.StatusOK, rec.Code)
			var got map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			for k, want := range tt.want {
				assert.Equal(t, want, got[k], k)
			}
		})
	}
}

func TestHandleUpdateTask_RejectsBacklogFromAnotherProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	otherProject := q.SeedProject(ownerID, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
		map[string]any{"title": "Fix bug", "backlogId": foreignBacklog.ID}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDeleteTask(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, ownerToken := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	other := q.SeedUser("intruder", "intruder@example.com")
	otherToken, err := s.sessions.Create(context.Background(), other.ID)
	require.NoError(t, err)

	// A non-owner gets 404 and the task survives.
	require.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/tasks/"+id, nil, otherToken).Code)
	require.Equal(t, http.StatusOK, doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, ownerToken).Code)

	require.Equal(t, http.StatusNoContent, doRequest(t, s, http.MethodDelete, "/api/v1/tasks/"+id, nil, ownerToken).Code)
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, ownerToken).Code)

	// Deleting twice is reported as "not found", not as success.
	assert.Equal(t, http.StatusNotFound, doRequest(t, s, http.MethodDelete, "/api/v1/tasks/"+id, nil, ownerToken).Code)
}

func TestHandleAssignTaskBacklog(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/assign-backlog",
		assignTaskBacklogRequest{BacklogID: &b.ID}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, b.ID.String(), body["backlogId"])

	rec = doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/assign-backlog",
		assignTaskBacklogRequest{BacklogID: nil}, token)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Nil(t, body["backlogId"])
}

func TestHandleAssignTaskBacklog_RejectsBacklogFromAnotherProject(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	otherProject := q.SeedProject(ownerID, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/assign-backlog",
		assignTaskBacklogRequest{BacklogID: &foreignBacklog.ID}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleAssignTaskBacklog_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/assign-backlog",
		assignTaskBacklogRequest{BacklogID: &b.ID}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCloseAndReopenTask_AreIdempotent(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/close", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var first map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.Equal(t, "closed", first["status"])
	require.NotNil(t, first["closedAt"])

	rec = doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/close", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var second map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.Equal(t, first["closedAt"], second["closedAt"])

	rec = doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/reopen", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var reopened map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reopened))
	assert.Equal(t, "open", reopened["status"])
	assert.Nil(t, reopened["closedAt"])

	rec = doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/reopen", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var reopenedAgain map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reopenedAgain))
	assert.Equal(t, "open", reopenedAgain["status"])
}

func TestHandleGetTask_IncludesAIContext(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Contains(t, body, "aiContext")
	aiContext, ok := body["aiContext"].(map[string]any)
	require.True(t, ok, "aiContext must be an object even before it is ever set")
	assert.Equal(t, "", aiContext["acceptanceCriteria"])
	assert.Nil(t, aiContext["updatedAt"])
}

func TestHandleUpsertTaskAIContext(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	first := doRequest(t, s, http.MethodPut, "/api/v1/tasks/"+id+"/ai-context", upsertTaskAIContextRequest{
		AcceptanceCriteria: "Given/When/Then",
		AIContext:          "Legacy payments module",
	}, token)
	require.Equal(t, http.StatusOK, first.Code)
	var firstBody map[string]any
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstBody))
	assert.Equal(t, "Given/When/Then", firstBody["acceptanceCriteria"])

	// A second call overwrites, it doesn't merge.
	second := doRequest(t, s, http.MethodPut, "/api/v1/tasks/"+id+"/ai-context", upsertTaskAIContextRequest{
		AcceptanceCriteria: "Updated",
	}, token)
	require.Equal(t, http.StatusOK, second.Code)
	var secondBody map[string]any
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondBody))
	assert.Equal(t, "Updated", secondBody["acceptanceCriteria"])

	getRec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, token)
	require.Equal(t, http.StatusOK, getRec.Code)
	var taskBody map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &taskBody))
	aiContext := taskBody["aiContext"].(map[string]any)
	assert.Equal(t, "Updated", aiContext["acceptanceCriteria"])
}

func TestHandleUpsertTaskAIContext_DoesNotChangeTaskUpdatedAt(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	before := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, token)
	require.Equal(t, http.StatusOK, before.Code)
	var beforeBody map[string]any
	require.NoError(t, json.Unmarshal(before.Body.Bytes(), &beforeBody))

	rec := doRequest(t, s, http.MethodPut, "/api/v1/tasks/"+id+"/ai-context", upsertTaskAIContextRequest{
		AcceptanceCriteria: "Given/When/Then",
	}, token)
	require.Equal(t, http.StatusOK, rec.Code)

	after := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+id, nil, token)
	require.Equal(t, http.StatusOK, after.Code)
	var afterBody map[string]any
	require.NoError(t, json.Unmarshal(after.Body.Bytes(), &afterBody))

	assert.Equal(t, beforeBody["updatedAt"], afterBody["updatedAt"])
}

func TestHandleUpsertTaskAIContext_RejectsFieldOverLengthLimit(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPut, "/api/v1/tasks/"+id+"/ai-context", upsertTaskAIContextRequest{
		AcceptanceCriteria: strings.Repeat("a", 20001),
	}, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUpsertTaskAIContext_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPut, "/api/v1/tasks/"+id+"/ai-context",
		upsertTaskAIContextRequest{AcceptanceCriteria: "Hijacked"}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCloseTask_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/close", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleMarkTaskDesignStarted_SetsTimestamp(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/design-started", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		DesignStartedAt         *time.Time `json:"designStartedAt"`
		ImplementationStartedAt *time.Time `json:"implementationStartedAt"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got.DesignStartedAt)
	assert.Nil(t, got.ImplementationStartedAt)
}

func TestHandleMarkTaskDesignStarted_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/design-started", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleMarkTaskImplementationStarted_SetsTimestamp(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/implementation-started", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		DesignStartedAt         *time.Time `json:"designStartedAt"`
		ImplementationStartedAt *time.Time `json:"implementationStartedAt"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Nil(t, got.DesignStartedAt)
	require.NotNil(t, got.ImplementationStartedAt)
}

func TestHandleMarkTaskImplementationStarted_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/implementation-started", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleRetryTaskSync_ReturnsConflictWhenNotFailed(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/sync-retry", nil, token)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleRetryTaskSync_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	id := q.SeedTask(p.ID, owner.ID, "Fix bug").ID.String()

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+id+"/sync-retry", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleRetryTaskSync_ResetsFailedTaskToPending(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")
	q.SeedSyncJobForTask(tsk.ID, p.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/sync-retry", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	gitlab, ok := body["gitlab"].(map[string]any)
	require.True(t, ok, "gitlab must be an object once the task has a sync job")
	assert.Equal(t, "pending", gitlab["syncStatus"])
}

func TestHandleBulkCreateTasks(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	body := bulkCreateTasksRequest{
		Tasks: []bulkTaskRequest{
			{Ref: "t1", Title: "Design"},
			{Ref: "t2", Title: "Implement", Priority: "high"},
		},
		Dependencies: []bulkDependencyRequest{
			{PredecessorRef: "t1", SuccessorRef: "t2"},
		},
	}
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks/bulk", body, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp struct {
		Tasks []struct {
			Ref  string         `json:"ref"`
			Task map[string]any `json:"task"`
		} `json:"tasks"`
		Dependencies []map[string]any `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Tasks, 2)
	require.Len(t, resp.Dependencies, 1)
	assert.Equal(t, "Design", resp.Tasks[0].Task["title"])
}

func TestHandleBulkCreateTasks_RejectsCyclicDependency(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	body := bulkCreateTasksRequest{
		Tasks: []bulkTaskRequest{
			{Ref: "t1", Title: "A"},
			{Ref: "t2", Title: "B"},
		},
		Dependencies: []bulkDependencyRequest{
			{PredecessorRef: "t1", SuccessorRef: "t2"},
			{PredecessorRef: "t2", SuccessorRef: "t1"},
		},
	}
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks/bulk", body, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// All-or-nothing rollback needs a real transaction to observe — see
	// TestBulkCreate_AllOrNothing_RealPostgres in internal/task's
	// integration tests; dbtest.FakeTxRunner runs its closure directly
	// against the fake with no rollback semantics, so it can't verify that
	// here.
}

func TestHandleBulkCreateTasks_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks/bulk",
		bulkCreateTasksRequest{Tasks: []bulkTaskRequest{{Ref: "t1", Title: "A"}}}, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListTasks_RejectsInvalidSizeQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?size=bogus", nil, token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ?size= narrows the project list and ?sort=size ranks biggest first, the
// same direction ?sort=priority runs.
func TestHandleListTasks_FiltersAndSortsBySizeQuery(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	for _, tc := range []struct{ title, size string }{
		{"Small one", "xs"},
		{"Huge one", "xl"},
		{"Middling one", "m"},
	} {
		created := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
			map[string]any{"title": tc.title, "size": tc.size}, token)
		require.Equal(t, http.StatusCreated, created.Code)
	}

	filtered := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?size=xl", nil, token)
	require.Equal(t, http.StatusOK, filtered.Code)
	var only []map[string]any
	require.NoError(t, json.Unmarshal(filtered.Body.Bytes(), &only))
	require.Len(t, only, 1)
	assert.Equal(t, "Huge one", only[0]["title"])
	assert.Equal(t, "xl", only[0]["size"])

	sorted := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks?sort=size", nil, token)
	require.Equal(t, http.StatusOK, sorted.Code)
	var ranked []map[string]any
	require.NoError(t, json.Unmarshal(sorted.Body.Bytes(), &ranked))
	require.Len(t, ranked, 3)
	assert.Equal(t, []any{"xl", "m", "xs"},
		[]any{ranked[0]["size"], ranked[1]["size"], ranked[2]["size"]})
}

// A task created without a size gets the middle value, and an unknown size
// is a 400 rather than being silently coerced.
func TestHandleCreateTask_DefaultsAndValidatesSize(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	defaulted := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "No size given"}, token)
	require.Equal(t, http.StatusCreated, defaulted.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(defaulted.Body.Bytes(), &body))
	assert.Equal(t, "m", body["size"])

	rejected := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "Bad size", "size": "gigantic"}, token)
	assert.Equal(t, http.StatusBadRequest, rejected.Code)
}

// The AI-facing context endpoint carries the size, so an agent knows how
// large the work is expected to be before it starts.
func TestHandleGetTaskContext_IncludesSize(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")

	created := doRequest(t, s, http.MethodPost, "/api/v1/projects/"+p.ID.String()+"/tasks",
		map[string]any{"title": "Big job", "size": "xl"}, token)
	require.Equal(t, http.StatusCreated, created.Code)
	var task map[string]any
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &task))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+task["id"].(string)+"/context", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "xl", body["size"])
}
