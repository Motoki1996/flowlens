package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flowlens/api/internal/apitoken"
	"github.com/flowlens/api/internal/taskcomment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCreateTaskComment_SessionAuth_SetsAuthorKindUser(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments",
		createTaskCommentRequest{Body: "Started looking into this."}, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var body taskcomment.TaskComment
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, taskcomment.AuthorKindUser, body.AuthorKind)
	require.NotNil(t, body.AuthorUserID)
	assert.Equal(t, ownerID, *body.AuthorUserID)
	assert.Nil(t, body.AuthorTokenID)
}

func TestHandleCreateTaskComment_BearerAuth_SetsAuthorKindAgent(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments",
		createTaskCommentRequest{Body: "Pushed a fix in MR !12."}, raw)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var body taskcomment.TaskComment
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, taskcomment.AuthorKindAgent, body.AuthorKind)
	assert.Nil(t, body.AuthorUserID)
	require.NotNil(t, body.AuthorTokenID)
}

// TestHandleCreateTaskComment_BearerAuth_ForeignProjectTaskGets404 pins the
// issue #103 acceptance criterion: a token must not be able to post to a
// task in a different project, even one owned by the same user.
func TestHandleCreateTaskComment_BearerAuth_ForeignProjectTaskGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	otherTask := q.SeedTask(otherProject.ID, owner.ID, "Other task")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+otherTask.ID.String()+"/comments",
		createTaskCommentRequest{Body: "Should not land"}, raw)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCreateTaskComment_BearerAuth_ReadOnlyTokenGets403(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	tsk := q.SeedTask(p.ID, owner.ID, "Fix bug")
	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeRead}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments",
		createTaskCommentRequest{Body: "Should not land"}, raw)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandleListTaskComments_SessionAuth(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")
	require.Equal(t, http.StatusCreated,
		doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments", createTaskCommentRequest{Body: "First"}, token).Code)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/comments", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var comments []taskcomment.TaskComment
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &comments))
	require.Len(t, comments, 1)
	assert.Equal(t, "First", comments[0].Body)
}

func TestHandleDeleteTaskComment_SessionAuth_OwnCommentSucceeds(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")
	createRec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments", createTaskCommentRequest{Body: "First"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created taskcomment.TaskComment
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/task-comments/"+created.ID.String(), nil, token)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestHandleDeleteTaskComment_SessionAuth_OtherUsersCommentGets403(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")
	createRec := doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments", createTaskCommentRequest{Body: "First"}, token)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created taskcomment.TaskComment
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	teammateID, teammateToken := loginSessionAs(t, s, q, "hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, teammateID, "member")

	rec := doRequest(t, s, http.MethodDelete, "/api/v1/task-comments/"+created.ID.String(), nil, teammateToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestHandleDeleteTaskComment_BearerAuth_ForeignProjectCommentGets404 pins
// the issue #103 acceptance criterion for the DELETE route: a token must
// not be able to reach a comment belonging to a different project, even one
// owned by the same user.
func TestHandleDeleteTaskComment_BearerAuth_ForeignProjectCommentGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	otherProject := q.SeedProject(owner.ID, "Beta")
	otherTask := q.SeedTask(otherProject.ID, owner.ID, "Other task")
	_, ownerRaw, err := s.apiTokens.Create(context.Background(), owner.ID, otherProject.ID, "Other bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)
	createRec := doBearerRequest(t, s, http.MethodPost, "/api/v1/tasks/"+otherTask.ID.String()+"/comments",
		createTaskCommentRequest{Body: "Done"}, ownerRaw)
	require.Equal(t, http.StatusCreated, createRec.Code)
	var created taskcomment.TaskComment
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	_, raw, err := s.apiTokens.Create(context.Background(), owner.ID, p.ID, "CI bot", []string{apitoken.ScopeWrite}, nil)
	require.NoError(t, err)

	rec := doBearerRequest(t, s, http.MethodDelete, "/api/v1/task-comments/"+created.ID.String(), nil, raw)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleGetTaskContext_IncludesRecentComments pins issue #103's context
// embedding requirement: GET /tasks/{taskID}/context returns the task's
// recent activity log alongside its usual AI-facing fields.
func TestHandleGetTaskContext_IncludesRecentComments(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	tsk := q.SeedTask(p.ID, ownerID, "Fix bug")
	require.Equal(t, http.StatusCreated,
		doRequest(t, s, http.MethodPost, "/api/v1/tasks/"+tsk.ID.String()+"/comments", createTaskCommentRequest{Body: "Started"}, token).Code)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/tasks/"+tsk.ID.String()+"/context", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body taskContextResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Comments, 1)
	assert.Equal(t, "Started", body.Comments[0].Body)
}
