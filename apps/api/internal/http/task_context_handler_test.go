package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doBearerRequest is doRequest for a project API token instead of a session
// cookie, the auth path GET /tasks/{taskID}/context and GET
// /projects/{projectID}/tasks/context also accept (docs/plans/issue-sync.md
// "AI-facing").
func doBearerRequest(t *testing.T, s *Server, method, path string, body any, rawToken string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if rawToken != "" {
		req.Header.Set("Authorization", "Bearer "+rawToken)
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestHandleGetTaskContext_SessionAuth(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	tsk := q.SeedTaskInBacklog(p.ID, b.ID, ownerID, "Fix bug")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Fix bug", body["title"])
	assert.Equal(t, "open", body["status"])
	assert.Equal(t, b.ID.String(), body["backlogId"])
	assert.Nil(t, body["gitlab"])
	assert.Contains(t, body, "gitlab")
}

func TestHandleGetTaskContext_SessionAuth_ForeignTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetTaskContext_NoAuthGets401(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleGetTaskContext_BearerAuth(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, raw)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Fix bug", body["title"])
}

func TestHandleGetTaskContext_BearerAuth_ForeignProjectTokenGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, otherProject.ID, "CI bot", nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, raw)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetTaskContext_UnknownTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	_, token := loginSession(t, s, q)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+uuid.New().String()+"/context", nil, token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleGetTaskContext_AIFieldsAreEmptyStringNotNull pins the acceptance
// criterion from issue #28: a task with no task_ai_contexts row yet must
// report its AI fields as "", never as a JSON null an AI client would need
// to special-case.
func TestHandleGetTaskContext_AIFieldsAreEmptyStringNotNull(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	for _, field := range []string{"acceptanceCriteria", "aiContext", "allowedScope", "forbiddenScope"} {
		require.Contains(t, body, field)
		assert.Equal(t, "", body[field], "field %q must be empty string, not null, when no AI context is set", field)
	}
}

func TestHandleGetTaskContext_IncludesAIContextAndGitlabProjectPath(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")

	_, err := s.tasks.UpsertAIContext(context.Background(), ownerID, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Given/When/Then",
		AllowedScope:       "internal/payments/**",
	})
	require.NoError(t, err)

	link := q.SeedLinkedGitlabProject(nil)
	q.SeedTaskGitlabLink(tsk.ID, link.ID, 7)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Given/When/Then", body["acceptanceCriteria"])
	assert.Equal(t, "internal/payments/**", body["allowedScope"])

	gitlabBody, ok := body["gitlab"].(map[string]any)
	require.True(t, ok, "gitlab must be an object once the task is linked")
	assert.Equal(t, "synced", gitlabBody["syncStatus"])
	assert.Equal(t, float64(7), gitlabBody["issueIid"])
	assert.Equal(t, link.PathWithNamespace, gitlabBody["projectPath"])
}

func TestHandleListTaskContexts_SessionAuth(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	q.SeedTask(p.ID, ownerID, "Fix bug")
	q.SeedTask(p.ID, ownerID, "Write docs")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Tasks    []task.Context `json:"tasks"`
		NextPage int            `json:"nextPage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Tasks, 2)
	assert.Zero(t, body.NextPage)
}

func TestHandleListTaskContexts_SessionAuth_ForeignProjectGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListTaskContexts_NoAuthGets401(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context", nil, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleListTaskContexts_BearerAuth(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context", nil, raw)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Tasks    []task.Context `json:"tasks"`
		NextPage int            `json:"nextPage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Tasks, 1)
}

func TestHandleListTaskContexts_BearerAuth_ForeignProjectTokenGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, otherProject.ID, "CI bot", nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context", nil, raw)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleListTaskContexts_FiltersByStatusAndBacklogID(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"no filter returns everything", "", []string{"Open task", "Closed task"}},
		{"status=open", "?status=open", []string{"Open task"}},
		{"status=closed", "?status=closed", []string{"Closed task"}},
		{"backlog_id scopes to one backlog", "?backlog_id={backlogID}", []string{"Open task"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			ownerID, token := loginSession(t, s, q)
			p := q.SeedProject(ownerID, "Alpha")
			b := q.SeedBacklog(p.ID, "Sprint 1")
			q.SeedTaskInBacklog(p.ID, b.ID, ownerID, "Open task")
			closed := q.SeedTask(p.ID, ownerID, "Closed task")
			_, err := s.tasks.Close(context.Background(), ownerID, closed.ID)
			require.NoError(t, err)

			query := tt.query
			if query == "?backlog_id={backlogID}" {
				query = "?backlog_id=" + b.ID.String()
			}

			rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context"+query, nil, token)
			require.Equal(t, http.StatusOK, rec.Code)

			var body struct {
				Tasks []task.Context `json:"tasks"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			titles := make([]string, len(body.Tasks))
			for i, tc := range body.Tasks {
				titles[i] = tc.Title
			}
			assert.ElementsMatch(t, tt.want, titles)
		})
	}
}

func TestHandleListTaskContexts_FiltersByUpdatedSince(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	q.SeedTask(p.ID, ownerID, "Fix bug")

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context?updated_since="+past, nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Tasks []task.Context `json:"tasks"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Tasks, 1, "a task updated after updated_since must be included")

	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context?updated_since="+future, nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.Tasks, "a task updated before updated_since must be excluded")
}

func TestHandleListTaskContexts_Paging(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	q.SeedTask(p.ID, ownerID, "Task 1")
	q.SeedTask(p.ID, ownerID, "Task 2")
	q.SeedTask(p.ID, ownerID, "Task 3")

	rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context?per_page=2", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var page1 struct {
		Tasks    []task.Context `json:"tasks"`
		NextPage int            `json:"nextPage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page1))
	assert.Len(t, page1.Tasks, 2)
	assert.Equal(t, 2, page1.NextPage)

	rec = doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context?per_page=2&page=2", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	var page2 struct {
		Tasks    []task.Context `json:"tasks"`
		NextPage int            `json:"nextPage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page2))
	assert.Len(t, page2.Tasks, 1)
	assert.Zero(t, page2.NextPage)
}

func TestHandleListTaskContexts_RejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"invalid backlog_id", "?backlog_id=not-a-uuid"},
		{"invalid status", "?status=bogus"},
		{"invalid updated_since", "?updated_since=not-a-timestamp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, q := newTestServer(t)
			ownerID, token := loginSession(t, s, q)
			p := q.SeedProject(ownerID, "Alpha")

			rec := doRequest(t, s, http.MethodGet, "/api/v1/projects/"+p.ID.String()+"/tasks/context"+tt.query, nil, token)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}
