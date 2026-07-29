package issuesync_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/issuesync"
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

// fixture bundles an issuesync Service with an in-memory querier and a
// GitLab connection + linked project already seeded, so each test can go
// straight to exercising a handler. GitLab calls go to fake instead of a
// real GitLab CE instance.
type fixture struct {
	svc    *issuesync.Service
	q      *dbtest.FakeQuerier
	linkID uuid.UUID
}

func newFixture(t *testing.T, fake *gitlab.FakeClient) fixture {
	t.Helper()
	q := dbtest.New()
	cipher := testCipher(t)
	encryptedToken, err := cipher.Encrypt("glpat-test-token")
	require.NoError(t, err)

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, encryptedToken)
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
	})
	require.NoError(t, err)

	svc := issuesync.NewService(q, cipher, func(string) gitlab.Client { return fake })
	return fixture{svc: svc, q: q, linkID: link.ID}
}

// makeJob builds a db.SyncJob carrying taskID and payload marshaled as JSON,
// the shape internal/task enqueues and this package's handlers decode.
// task_gitlab_links only needs taskID's UUID to exist, never a real tasks
// row: internal/issuesync never joins to tasks.
func makeJob(taskID uuid.UUID, kind string, payload any) db.SyncJob {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return db.SyncJob{
		ID:      uuid.New(),
		TaskID:  pgtype.UUID{Bytes: taskID, Valid: true},
		Kind:    kind,
		Payload: body,
	}
}

func TestHandleIssueCreate_CreatesIssueAndLink(t *testing.T) {
	fake := &gitlab.FakeClient{Issue: &gitlab.Issue{
		ID: 501, IID: 7, WebURL: "https://gitlab.example.com/group/demo/-/issues/7", UpdatedAt: time.Now(),
	}}
	f := newFixture(t, fake)
	taskID := uuid.New()

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	job := makeJob(taskID, issuesync.KindIssueCreate, issuesync.CreatePayload{
		LinkedGitlabProjectID: f.linkID,
		IssuePayload: gitlab.IssuePayload{
			Title:       "Fix bug",
			Description: "does the thing",
			Labels:      []string{"bug"},
			DueDate:     &due,
			AssigneeIDs: []int64{42},
		},
	})

	require.NoError(t, f.svc.HandleIssueCreate(context.Background(), job))

	require.Len(t, fake.CallLog, 1)
	assert.Equal(t, "CreateIssue", fake.CallLog[0].Method)
	assert.Equal(t, int64(100), fake.CallLog[0].Args[1])
	sentPayload := fake.CallLog[0].Args[2].(gitlab.IssuePayload)
	assert.Equal(t, "Fix bug", sentPayload.Title)
	assert.Equal(t, []string{"bug"}, sentPayload.Labels)

	link, err := f.q.GetTaskGitlabLinkByTaskID(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, int64(501), link.GitlabIssueID)
	assert.Equal(t, int64(7), link.GitlabIssueIid)
	assert.Equal(t, f.linkID, link.LinkedGitlabProjectID)
	assert.Equal(t, "synced", link.SyncStatus)
	assert.NotEmpty(t, link.LastPushedFingerprint)
}

func TestHandleIssueCreate_IsIdempotent_SecondRunDoesNotCreateAnotherIssue(t *testing.T) {
	fake := &gitlab.FakeClient{Issue: &gitlab.Issue{ID: 501, IID: 7, UpdatedAt: time.Now()}}
	f := newFixture(t, fake)
	taskID := uuid.New()
	job := makeJob(taskID, issuesync.KindIssueCreate, issuesync.CreatePayload{
		LinkedGitlabProjectID: f.linkID,
		IssuePayload:          gitlab.IssuePayload{Title: "Fix bug"},
	})

	require.NoError(t, f.svc.HandleIssueCreate(context.Background(), job))
	// Simulate the job running a second time (e.g. a duplicate execution or
	// a retry after a crash that landed the link but not the sync_jobs
	// status update) — it must not create a second GitLab issue.
	require.NoError(t, f.svc.HandleIssueCreate(context.Background(), job))

	createCalls := 0
	for _, c := range fake.CallLog {
		if c.Method == "CreateIssue" {
			createCalls++
		}
	}
	assert.Equal(t, 1, createCalls)
}

func TestHandleIssueUpdate_MapsFieldsAndMarksSynced(t *testing.T) {
	fake := &gitlab.FakeClient{Issue: &gitlab.Issue{ID: 501, IID: 7, UpdatedAt: time.Now()}}
	f := newFixture(t, fake)
	taskID := uuid.New()
	f.q.SeedTaskGitlabLink(taskID, f.linkID, 7)

	title := "Fix bug harder"
	description := "more detail"
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	job := makeJob(taskID, issuesync.KindIssueUpdate, issuesync.UpdatePayload{
		UpdateIssuePayload: gitlab.UpdateIssuePayload{
			Title:       &title,
			Description: &description,
			Labels:      []string{"bug", "urgent"},
			DueDate:     &due,
			AssigneeIDs: []int64{42},
		},
	})

	require.NoError(t, f.svc.HandleIssueUpdate(context.Background(), job))

	require.Len(t, fake.CallLog, 1)
	assert.Equal(t, "UpdateIssue", fake.CallLog[0].Method)
	assert.Equal(t, int64(100), fake.CallLog[0].Args[1])
	assert.Equal(t, int64(7), fake.CallLog[0].Args[2])
	sentPayload := fake.CallLog[0].Args[3].(gitlab.UpdateIssuePayload)
	require.NotNil(t, sentPayload.Title)
	assert.Equal(t, "Fix bug harder", *sentPayload.Title)
	assert.Equal(t, []string{"bug", "urgent"}, sentPayload.Labels)
	assert.Empty(t, sentPayload.StateEvent, "a plain field update must not also change state")

	link, err := f.q.GetTaskGitlabLinkByTaskID(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "synced", link.SyncStatus)
	assert.Empty(t, link.LastError)
	assert.NotEmpty(t, link.LastPushedFingerprint)
}

func TestHandleIssueUpdate_NoOpWhenTaskHasNoLink(t *testing.T) {
	fake := &gitlab.FakeClient{}
	f := newFixture(t, fake)
	taskID := uuid.New()
	job := makeJob(taskID, issuesync.KindIssueUpdate, issuesync.UpdatePayload{})

	require.NoError(t, f.svc.HandleIssueUpdate(context.Background(), job))
	assert.Empty(t, fake.CallLog)
}

func TestHandleIssueUpdate_MarksLinkFailedOnGitlabError(t *testing.T) {
	fake := &gitlab.FakeClient{UpdateIssueErr: errors.New("gitlab unavailable")}
	f := newFixture(t, fake)
	taskID := uuid.New()
	f.q.SeedTaskGitlabLink(taskID, f.linkID, 7)

	title := "x"
	job := makeJob(taskID, issuesync.KindIssueUpdate, issuesync.UpdatePayload{
		UpdateIssuePayload: gitlab.UpdateIssuePayload{Title: &title},
	})

	err := f.svc.HandleIssueUpdate(context.Background(), job)
	require.Error(t, err)

	link, getErr := f.q.GetTaskGitlabLinkByTaskID(context.Background(), taskID)
	require.NoError(t, getErr)
	assert.Equal(t, "failed", link.SyncStatus)
	assert.Contains(t, link.LastError, "gitlab unavailable")
}

func TestHandleIssueClose_SendsCloseStateEvent(t *testing.T) {
	fake := &gitlab.FakeClient{Issue: &gitlab.Issue{ID: 501, IID: 7, UpdatedAt: time.Now()}}
	f := newFixture(t, fake)
	taskID := uuid.New()
	f.q.SeedTaskGitlabLink(taskID, f.linkID, 7)

	job := makeJob(taskID, issuesync.KindIssueClose, nil)
	require.NoError(t, f.svc.HandleIssueClose(context.Background(), job))

	require.Len(t, fake.CallLog, 1)
	sentPayload := fake.CallLog[0].Args[3].(gitlab.UpdateIssuePayload)
	assert.Equal(t, "close", sentPayload.StateEvent)

	link, err := f.q.GetTaskGitlabLinkByTaskID(context.Background(), taskID)
	require.NoError(t, err)
	assert.Equal(t, "synced", link.SyncStatus)
}

func TestHandleIssueReopen_SendsReopenStateEvent(t *testing.T) {
	fake := &gitlab.FakeClient{Issue: &gitlab.Issue{ID: 501, IID: 7, UpdatedAt: time.Now()}}
	f := newFixture(t, fake)
	taskID := uuid.New()
	f.q.SeedTaskGitlabLink(taskID, f.linkID, 7)

	job := makeJob(taskID, issuesync.KindIssueReopen, nil)
	require.NoError(t, f.svc.HandleIssueReopen(context.Background(), job))

	require.Len(t, fake.CallLog, 1)
	sentPayload := fake.CallLog[0].Args[3].(gitlab.UpdateIssuePayload)
	assert.Equal(t, "reopen", sentPayload.StateEvent)
}

func TestHandleIssueClose_NoOpWhenTaskHasNoLink(t *testing.T) {
	fake := &gitlab.FakeClient{}
	f := newFixture(t, fake)
	taskID := uuid.New()

	require.NoError(t, f.svc.HandleIssueClose(context.Background(), makeJob(taskID, issuesync.KindIssueClose, nil)))
	assert.Empty(t, fake.CallLog)
}
