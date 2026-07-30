package http

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListWebhookEvents_ReturnsEventsWithoutPayload(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := q.SeedWebhookEvent(link.ID, []byte(`{"secret":"do-not-leak"}`))
	event.Status = "failed"
	q.SetWebhookEventForTest(event)

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Events   []webhookevent.Event `json:"events"`
		NextPage int                  `json:"nextPage"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Events, 1)
	assert.Equal(t, event.ID, body.Events[0].ID)
	assert.Equal(t, "failed", body.Events[0].Status)
	assert.NotContains(t, rec.Body.String(), "do-not-leak", "the list view must never include the raw payload")
}

func TestHandleListWebhookEvents_ForeignLinkGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetWebhookEvent_ReturnsPayload(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := q.SeedWebhookEvent(link.ID, []byte(`{"object_attributes":{"iid":1}}`))

	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events/"+event.ID.String(), nil, token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"iid":1`)
}

func TestHandleGetWebhookEvent_ForeignLinkGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := q.SeedWebhookEvent(link.ID, []byte(`{}`))

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events/"+event.ID.String(), nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleRetryWebhookEvent_ResetsFailedToPending(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := q.SeedWebhookEvent(link.ID, []byte(`{}`))
	event.Status = "failed"
	event.ErrorMessage = "gitlab unreachable"
	event.ProcessedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	q.SetWebhookEventForTest(event)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events/"+event.ID.String()+"/retry", nil, token)
	require.Equal(t, http.StatusOK, rec.Code)

	stored, ok := q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "pending", stored.Status)
	assert.Equal(t, "", stored.ErrorMessage)
}

func TestHandleRetryWebhookEvent_ConflictWhenNotFailed(t *testing.T) {
	s, q := newTestServer(t)
	ownerID, token := loginSession(t, s, q)
	p := q.SeedProject(ownerID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := q.SeedWebhookEvent(link.ID, []byte(`{}`))

	rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events/"+event.ID.String()+"/retry", nil, token)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestHandleRetryWebhookEvent_ForeignLinkGets404(t *testing.T) {
	s, q := newTestServer(t)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(t.Context(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := q.SeedWebhookEvent(link.ID, []byte(`{}`))
	event.Status = "failed"
	q.SetWebhookEventForTest(event)

	_, intruderToken := loginSession(t, s, q)
	rec := doRequest(t, s, http.MethodPost, "/api/v1/linked-gitlab-projects/"+link.ID.String()+"/webhook-events/"+event.ID.String()+"/retry", nil, intruderToken)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
