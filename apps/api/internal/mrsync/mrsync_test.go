package mrsync_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/mrsync"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New([]byte("01234567890123456789012345678901"[:32]))
	require.NoError(t, err)
	return c
}

// fixture bundles an mrsync Service backed by an in-memory querier with an
// owner, project, GitLab connection, linked GitLab project and repository
// already seeded, mirroring internal/projectsync's own test fixture.
type fixture struct {
	svc     *mrsync.Service
	q       *dbtest.FakeQuerier
	fake    *gitlab.FakeClient
	link    db.LinkedGitlabProject
	repo    db.Repository
	project db.Project
}

func newFixture(t *testing.T, fake *gitlab.FakeClient) fixture {
	t.Helper()
	q := dbtest.New()
	cipher := testCipher(t)
	encryptedToken, err := cipher.Encrypt("glpat-test-token")
	require.NoError(t, err)
	txRunner := dbtest.FakeTxRunner{Q: q}

	svc := mrsync.NewService(q, txRunner, cipher, func(string) gitlab.Client { return fake })

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, encryptedToken)

	ctx := context.Background()
	link, err := q.CreateLinkedGitlabProject(ctx, db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/project",
		Name:               "project",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)

	repo, err := mrsync.EnsureRepository(ctx, q, link)
	require.NoError(t, err)

	return fixture{svc: svc, q: q, fake: fake, link: link, repo: repo, project: p}
}

// runImport creates a fresh 'running' repository_sync_runs row for f.repo
// and executes HandleImport against it directly — bypassing
// EnqueueImport/the worker, so tests can drive one run at a time
// deterministically.
func (f fixture) runImport(t *testing.T) db.RepositorySyncRun {
	t.Helper()
	ctx := context.Background()
	run, err := f.q.CreateRepositorySyncRun(ctx, db.CreateRepositorySyncRunParams{RepositoryID: f.repo.ID, Kind: mrsync.RunKindInitialImport})
	require.NoError(t, err)

	payload, err := json.Marshal(mrsync.RunPayload{SyncRunID: run.ID, Full: true})
	require.NoError(t, err)
	job := db.SyncJob{ID: uuid.New(), ProjectID: f.project.ID, Kind: mrsync.KindMRImport, Payload: payload}

	require.NoError(t, f.svc.HandleImport(ctx, job))

	completed, err := f.q.GetRepositorySyncRunByID(ctx, run.ID)
	require.NoError(t, err)
	return completed
}

func mergeRequest(iid int64, title, state, sourceBranch, description string, updatedAt time.Time) gitlab.MergeRequest {
	return gitlab.MergeRequest{
		ID:           iid + 1000,
		IID:          iid,
		Title:        title,
		Description:  description,
		State:        state,
		Author:       gitlab.User{ID: 1, Username: "octocat"},
		SourceBranch: sourceBranch,
		TargetBranch: "main",
		WebURL:       "https://gitlab.example.com/group/project/-/merge_requests/1",
		CreatedAt:    updatedAt,
		UpdatedAt:    updatedAt,
	}
}

func TestHandleImport_CreatesMergeRequests(t *testing.T) {
	now := time.Now()
	fake := &gitlab.FakeClient{
		MergeRequests: []gitlab.MergeRequest{mergeRequest(1, "first mr", "opened", "feature-1", "", now)},
		MergeRequest:  ptr(mergeRequest(1, "first mr", "opened", "feature-1", "", now)),
	}
	f := newFixture(t, fake)

	run := f.runImport(t)

	assert.Equal(t, "succeeded", run.Status)
	assert.EqualValues(t, 1, run.MrsSeen)
	assert.EqualValues(t, 1, run.MrsCreated)
	assert.EqualValues(t, 0, run.MrsUpdated)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(context.Background(), 1001)
	require.NoError(t, err)
	assert.Equal(t, "first mr", mr.Title)
	assert.Equal(t, "opened", mr.State)
	assert.EqualValues(t, 1, mr.Number)
}

func TestHandleImport_RunTwice_NoDuplicatesAndStaleSkipped(t *testing.T) {
	now := time.Now()
	fake := &gitlab.FakeClient{
		MergeRequests: []gitlab.MergeRequest{mergeRequest(1, "first mr", "opened", "feature-1", "", now)},
		MergeRequest:  ptr(mergeRequest(1, "first mr", "opened", "feature-1", "", now)),
	}
	f := newFixture(t, fake)

	first := f.runImport(t)
	require.EqualValues(t, 1, first.MrsCreated)

	// A second run over the same, unchanged merge request must not create a
	// duplicate row, and — since its updated_at hasn't advanced — must not
	// count as an update either (the same stale guard idempotency
	// internal/projectsync relies on for issues).
	second := f.runImport(t)
	assert.EqualValues(t, 1, second.MrsSeen)
	assert.EqualValues(t, 0, second.MrsCreated)
	assert.EqualValues(t, 0, second.MrsUpdated)
}

func TestHandleImport_UpdatesExistingMergeRequest(t *testing.T) {
	firstUpdate := time.Now().Add(-time.Hour)
	fake := &gitlab.FakeClient{
		MergeRequests: []gitlab.MergeRequest{mergeRequest(1, "first mr", "opened", "feature-1", "", firstUpdate)},
		MergeRequest:  ptr(mergeRequest(1, "first mr", "opened", "feature-1", "", firstUpdate)),
	}
	f := newFixture(t, fake)
	f.runImport(t)

	laterUpdate := time.Now()
	merged := mergeRequest(1, "first mr", "merged", "feature-1", "", laterUpdate)
	fake.MergeRequests = []gitlab.MergeRequest{merged}
	fake.MergeRequest = ptr(merged)

	run := f.runImport(t)
	assert.EqualValues(t, 1, run.MrsUpdated)
	assert.EqualValues(t, 0, run.MrsCreated)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(context.Background(), 1001)
	require.NoError(t, err)
	assert.Equal(t, "merged", mr.State)
}

func TestHandleImport_PaginatesAllPages(t *testing.T) {
	now := time.Now()
	fake := &gitlab.FakeClient{
		MergeRequestsPages: [][]gitlab.MergeRequest{
			{mergeRequest(1, "one", "opened", "feature-1", "", now)},
			{mergeRequest(2, "two", "opened", "feature-2", "", now)},
		},
		MergeRequestByIID: map[int64]*gitlab.MergeRequest{
			1: ptr(mergeRequest(1, "one", "opened", "feature-1", "", now)),
			2: ptr(mergeRequest(2, "two", "opened", "feature-2", "", now)),
		},
	}
	f := newFixture(t, fake)

	run := f.runImport(t)
	assert.EqualValues(t, 2, run.MrsSeen)
	assert.EqualValues(t, 2, run.MrsCreated)
}

// linkTask seeds a task linked to issue iid on f's own linked GitLab
// project, for task-linking tests.
func (f fixture) linkTask(t *testing.T, iid int64) db.Task {
	t.Helper()
	owner := f.project.OwnerUserID
	task := f.q.SeedTask(f.project.ID, owner, "the linked task")
	f.q.SeedTaskGitlabLink(task.ID, f.link.ID, iid)
	return task
}

func TestHandleImport_LinksTaskFromClosesKeywordInDescription(t *testing.T) {
	now := time.Now()
	fake := &gitlab.FakeClient{
		MergeRequests: []gitlab.MergeRequest{mergeRequest(1, "fix", "opened", "feature-1", "Closes #42", now)},
		MergeRequest:  ptr(mergeRequest(1, "fix", "opened", "feature-1", "Closes #42", now)),
	}
	f := newFixture(t, fake)
	task := f.linkTask(t, 42)

	f.runImport(t)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(context.Background(), 1001)
	require.NoError(t, err)
	require.True(t, mr.TaskID.Valid)
	assert.Equal(t, task.ID, uuid.UUID(mr.TaskID.Bytes))
}

func TestHandleImport_LinksTaskFromBranchName(t *testing.T) {
	now := time.Now()
	fake := &gitlab.FakeClient{
		MergeRequests: []gitlab.MergeRequest{mergeRequest(1, "fix", "opened", "42-fix-thing", "", now)},
		MergeRequest:  ptr(mergeRequest(1, "fix", "opened", "42-fix-thing", "", now)),
	}
	f := newFixture(t, fake)
	task := f.linkTask(t, 42)

	f.runImport(t)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(context.Background(), 1001)
	require.NoError(t, err)
	require.True(t, mr.TaskID.Valid)
	assert.Equal(t, task.ID, uuid.UUID(mr.TaskID.Bytes))
}

func TestHandleImport_UnlinkableMergeRequest_LeavesTaskIDUnset(t *testing.T) {
	now := time.Now()
	fake := &gitlab.FakeClient{
		MergeRequests: []gitlab.MergeRequest{mergeRequest(1, "fix", "opened", "some-branch", "no reference here", now)},
		MergeRequest:  ptr(mergeRequest(1, "fix", "opened", "some-branch", "no reference here", now)),
	}
	f := newFixture(t, fake)
	f.runImport(t)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(context.Background(), 1001)
	require.NoError(t, err)
	assert.False(t, mr.TaskID.Valid)
}

func ptr[T any](v T) *T { return &v }
