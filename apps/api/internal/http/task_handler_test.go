package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flowlens/api/internal/issuesync"
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

	t.Run("owner can update, including position", func(t *testing.T) {
		rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
			map[string]any{"title": "Renamed", "description": "new", "position": 3}, ownerToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "Renamed", body["title"])
		assert.Equal(t, "new", body["description"])
		assert.Equal(t, float64(3), body["position"])
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

// PATCH is a partial update at the wire level too: a key absent from the
// JSON body leaves the field alone, and an explicit null clears a nullable
// one. The web edit form relies on this to send only what it shows.
func TestHandleUpdateTask_AppliesOnlyKeysPresentInBody(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	id := q.SeedTask(p.ID, ownerID, "Fix bug").ID.String()

	rec := doRequest(t, s, http.MethodPatch, "/api/v1/tasks/"+id,
		map[string]any{"title": "Renamed", "description": "original", "position": 3, "dueOn": "2026-09-01T00:00:00Z"}, token)
	require.Equal(t, http.StatusOK, rec.Code)

	tests := []struct {
		name string
		body map[string]any
		want map[string]any
	}{
		{
			name: "title only keeps description, position and due date",
			body: map[string]any{"title": "Renamed again"},
			want: map[string]any{"title": "Renamed again", "description": "original", "position": float64(3), "dueOn": "2026-09-01T00:00:00Z"},
		},
		{
			name: "explicit null clears the due date",
			body: map[string]any{"dueOn": nil},
			want: map[string]any{"title": "Renamed again", "description": "original", "position": float64(3), "dueOn": nil},
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
		AllowedScope:       "internal/payments/**",
		ForbiddenScope:     "internal/auth/**",
	}, token)
	require.Equal(t, http.StatusOK, first.Code)
	var firstBody map[string]any
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstBody))
	assert.Equal(t, "Given/When/Then", firstBody["acceptanceCriteria"])
	assert.Equal(t, "internal/payments/**", firstBody["allowedScope"])

	// A second call overwrites, it doesn't merge.
	second := doRequest(t, s, http.MethodPut, "/api/v1/tasks/"+id+"/ai-context", upsertTaskAIContextRequest{
		AcceptanceCriteria: "Updated",
	}, token)
	require.Equal(t, http.StatusOK, second.Code)
	var secondBody map[string]any
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &secondBody))
	assert.Equal(t, "Updated", secondBody["acceptanceCriteria"])
	assert.Equal(t, "", secondBody["allowedScope"])

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
