package webhookevent_test

import (
	"context"
	"testing"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New([]byte("01234567890123456789012345678901"[:32]))
	require.NoError(t, err)
	return c
}

// fixture bundles a webhookevent Service backed by an in-memory querier with
// an owner, project, GitLab connection and linked GitLab project already
// seeded, so each test can go straight to recording/listing events.
type fixture struct {
	svc     *webhookevent.Service
	q       *dbtest.FakeQuerier
	ownerID uuid.UUID
	link    db.LinkedGitlabProject
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	q := dbtest.New()
	svc := webhookevent.NewService(q, testCipher(t))

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)

	return fixture{svc: svc, q: q, ownerID: owner.ID, link: link}
}

// seedEvent inserts a webhook_events row directly in the given status, for
// tests that don't need to exercise Record itself.
func (f fixture) seedEvent(t *testing.T, status string, processedAt time.Time) db.WebhookEvent {
	t.Helper()
	e := f.q.SeedWebhookEvent(f.link.ID, []byte(`{"object_attributes":{"iid":1}}`))
	e.Status = status
	if !processedAt.IsZero() {
		e.ProcessedAt = pgtype.Timestamptz{Time: processedAt, Valid: true}
	}
	f.q.SetWebhookEventForTest(e)
	return e
}

func TestService_List_NewestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first := f.seedEvent(t, webhookevent.StatusProcessed, time.Now())
	time.Sleep(time.Millisecond)
	second := f.seedEvent(t, webhookevent.StatusFailed, time.Time{})

	page, err := f.svc.List(ctx, f.ownerID, f.link.ID, webhookevent.ListParams{})
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	assert.Equal(t, second.ID, page.Events[0].ID, "the most recently received event is listed first")
	assert.Equal(t, first.ID, page.Events[1].ID)
	assert.Equal(t, 0, page.NextPage)
}

func TestService_List_StatusFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.seedEvent(t, webhookevent.StatusProcessed, time.Now())
	failed := f.seedEvent(t, webhookevent.StatusFailed, time.Time{})

	page, err := f.svc.List(ctx, f.ownerID, f.link.ID, webhookevent.ListParams{Status: webhookevent.StatusFailed})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.Equal(t, failed.ID, page.Events[0].ID)
}

func TestService_List_Pagination(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		f.seedEvent(t, webhookevent.StatusPending, time.Time{})
	}

	page, err := f.svc.List(ctx, f.ownerID, f.link.ID, webhookevent.ListParams{PerPage: 2})
	require.NoError(t, err)
	assert.Len(t, page.Events, 2)
	assert.Equal(t, 2, page.NextPage)

	next, err := f.svc.List(ctx, f.ownerID, f.link.ID, webhookevent.ListParams{PerPage: 2, Page: page.NextPage})
	require.NoError(t, err)
	assert.Len(t, next.Events, 1)
	assert.Equal(t, 0, next.NextPage)
}

func TestService_List_NotFoundForForeignLink(t *testing.T) {
	f := newFixture(t)
	stranger := f.q.SeedUser("stranger", "stranger@example.com")

	_, err := f.svc.List(context.Background(), stranger.ID, f.link.ID, webhookevent.ListParams{})
	assert.ErrorIs(t, err, webhookevent.ErrNotFound)
}

func TestService_Get_ReturnsPayload(t *testing.T) {
	f := newFixture(t)
	event := f.seedEvent(t, webhookevent.StatusFailed, time.Time{})

	got, err := f.svc.Get(context.Background(), f.ownerID, f.link.ID, event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, got.ID)
	assert.JSONEq(t, string(event.Payload), string(got.Payload))
}

func TestService_Get_NotFoundForForeignLink(t *testing.T) {
	f := newFixture(t)
	stranger := f.q.SeedUser("stranger", "stranger@example.com")
	event := f.seedEvent(t, webhookevent.StatusFailed, time.Time{})

	_, err := f.svc.Get(context.Background(), stranger.ID, f.link.ID, event.ID)
	assert.ErrorIs(t, err, webhookevent.ErrNotFound)
}

func TestService_Get_NotFoundForEventUnderAnotherLink(t *testing.T) {
	f := newFixture(t)
	otherConn := f.q.SeedGitlabConnection(f.q.SeedProject(f.ownerID, "Beta").ID, []byte("encrypted-token-2"))
	otherLink, err := f.q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: otherConn.ID,
		GitlabProjectID:    200,
		PathWithNamespace:  "group/other",
		Name:               "other",
		SyncScope:          "all",
	})
	require.NoError(t, err)
	event := f.seedEvent(t, webhookevent.StatusFailed, time.Time{})

	_, err = f.svc.Get(context.Background(), f.ownerID, otherLink.ID, event.ID)
	assert.ErrorIs(t, err, webhookevent.ErrNotFound)
}

func TestService_Retry_MovesFailedEventToPending(t *testing.T) {
	f := newFixture(t)
	event := f.seedEvent(t, webhookevent.StatusFailed, time.Now())

	got, err := f.svc.Retry(context.Background(), f.ownerID, f.link.ID, event.ID)
	require.NoError(t, err)
	assert.Equal(t, webhookevent.StatusPending, got.Status)
	assert.Nil(t, got.ProcessedAt)

	stored, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "pending", stored.Status)
}

func TestService_Retry_NotFailedWhenAlreadyPending(t *testing.T) {
	f := newFixture(t)
	event := f.seedEvent(t, webhookevent.StatusPending, time.Time{})

	_, err := f.svc.Retry(context.Background(), f.ownerID, f.link.ID, event.ID)
	assert.ErrorIs(t, err, webhookevent.ErrNotFailed)
}

func TestService_Retry_NotFoundForForeignLink(t *testing.T) {
	f := newFixture(t)
	stranger := f.q.SeedUser("stranger", "stranger@example.com")
	event := f.seedEvent(t, webhookevent.StatusFailed, time.Time{})

	_, err := f.svc.Retry(context.Background(), stranger.ID, f.link.ID, event.ID)
	assert.ErrorIs(t, err, webhookevent.ErrNotFound)
}

func TestService_CleanupProcessed_DeletesOnlyOldProcessedEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	old := f.seedEvent(t, webhookevent.StatusProcessed, time.Now().Add(-40*24*time.Hour))
	recent := f.seedEvent(t, webhookevent.StatusProcessed, time.Now())
	oldFailed := f.seedEvent(t, webhookevent.StatusFailed, time.Now().Add(-40*24*time.Hour))
	oldSkipped := f.seedEvent(t, webhookevent.StatusSkipped, time.Now().Add(-40*24*time.Hour))
	pending := f.seedEvent(t, webhookevent.StatusPending, time.Time{})

	deleted, err := f.svc.CleanupProcessed(ctx, webhookevent.DefaultRetentionPeriod)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	_, ok := f.q.GetWebhookEvent(old.ID)
	assert.False(t, ok, "old processed event should be deleted")

	for _, kept := range []db.WebhookEvent{recent, oldFailed, oldSkipped, pending} {
		_, ok := f.q.GetWebhookEvent(kept.ID)
		assert.True(t, ok, "event %s should be kept", kept.ID)
	}
}
