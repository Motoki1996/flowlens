package projectsync_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectsync"
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

// fixture bundles a projectsync Service backed by an in-memory querier with
// an owner, project, GitLab connection and linked GitLab project already
// seeded, so each test can go straight to enqueueing/handling a run. Its
// GitLab calls all go to fake instead of a real GitLab CE instance.
type fixture struct {
	svc     *projectsync.Service
	q       *dbtest.FakeQuerier
	fake    *gitlab.FakeClient
	link    db.LinkedGitlabProject
	project db.Project
}

func newFixture(t *testing.T, fake *gitlab.FakeClient) fixture {
	t.Helper()
	q := dbtest.New()
	cipher := testCipher(t)
	encryptedToken, err := cipher.Encrypt("glpat-test-token")
	require.NoError(t, err)
	txRunner := dbtest.FakeTxRunner{Q: q}

	svc := projectsync.NewService(q, txRunner, project.NewService(q), cipher, func(string) gitlab.Client { return fake })

	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, encryptedToken)

	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/project",
		Name:               "project",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)

	return fixture{svc: svc, q: q, fake: fake, link: link, project: p}
}

// runImport creates a fresh 'running' gitlab_sync_runs row for f.link and
// executes HandleImport against it directly — bypassing EnqueueImport/the
// worker, so tests can drive one run at a time deterministically.
func (f fixture) runImport(t *testing.T, full bool) db.GitlabSyncRun {
	t.Helper()
	return f.runKind(t, projectsync.RunKindInitialImport, projectsync.KindProjectImport, full)
}

func (f fixture) runResync(t *testing.T, full bool) db.GitlabSyncRun {
	t.Helper()
	return f.runKind(t, projectsync.RunKindManualResync, projectsync.KindProjectResync, full)
}

func (f fixture) runKind(t *testing.T, runKind, jobKind string, full bool) db.GitlabSyncRun {
	t.Helper()
	ctx := context.Background()
	run, err := f.q.CreateGitlabSyncRun(ctx, db.CreateGitlabSyncRunParams{LinkedGitlabProjectID: f.link.ID, Kind: runKind})
	require.NoError(t, err)

	payload, err := json.Marshal(projectsync.RunPayload{SyncRunID: run.ID, Full: full})
	require.NoError(t, err)
	job := db.SyncJob{ID: uuid.New(), ProjectID: f.project.ID, Kind: jobKind, Payload: payload}

	if jobKind == projectsync.KindProjectResync {
		require.NoError(t, f.svc.HandleResync(ctx, job))
	} else {
		require.NoError(t, f.svc.HandleImport(ctx, job))
	}

	completed, err := f.q.GetGitlabSyncRunByID(ctx, run.ID)
	require.NoError(t, err)
	return completed
}

func (f fixture) tasksInProject(t *testing.T) []db.Task {
	t.Helper()
	tasks, err := f.q.ListTasksByProject(context.Background(), db.ListTasksByProjectParams{ProjectID: f.project.ID})
	require.NoError(t, err)
	return tasks
}

func issue(iid int64, title, state string, updatedAt time.Time, labels ...string) gitlab.Issue {
	return gitlab.Issue{
		ID:        iid + 1000,
		IID:       iid,
		Title:     title,
		State:     state,
		Labels:    labels,
		WebURL:    "https://gitlab.example.com/group/project/-/issues/" + title,
		UpdatedAt: updatedAt,
	}
}

func TestHandleImport_CreatesUnclassifiedTasksFromScopedIssues(t *testing.T) {
	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{
		issue(1, "first bug", "opened", time.Now(), "bug"),
		issue(2, "second issue", "closed", time.Now()),
	}}
	f := newFixture(t, fake)

	run := f.runImport(t, true)

	assert.Equal(t, "succeeded", run.Status)
	assert.EqualValues(t, 2, run.IssuesSeen)
	assert.EqualValues(t, 2, run.IssuesCreated)
	assert.EqualValues(t, 0, run.IssuesUpdated)

	tasks := f.tasksInProject(t)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		assert.False(t, task.BacklogID.Valid, "imported task must be unclassified (backlog_id NULL)")
	}
}

func TestHandleImport_OutOfScopeIssue_Skipped(t *testing.T) {
	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{
		issue(1, "in scope", "opened", time.Now(), "bug"),
		issue(2, "out of scope", "opened", time.Now(), "chore"),
	}}
	f := newFixture(t, fake)
	// CreateLinkedGitlabProject seeded scope "all"; narrow it to "labels"
	// via the same update path Update() uses.
	_, err := f.q.UpdateLinkedGitlabProjectSyncScopeForOwner(context.Background(), db.UpdateLinkedGitlabProjectSyncScopeForOwnerParams{
		ID:          f.link.ID,
		OwnerUserID: f.tasksOwner(t),
		SyncScope:   "labels",
		SyncLabels:  []string{"bug"},
	})
	require.NoError(t, err)

	run := f.runImport(t, true)

	assert.EqualValues(t, 1, run.IssuesSeen, "only the in-scope issue is counted as seen")
	assert.EqualValues(t, 1, run.IssuesCreated)
	require.Len(t, f.tasksInProject(t), 1)
}

// tasksOwner returns the project's owner, for the ownership-scoped
// UpdateLinkedGitlabProjectSyncScopeForOwner call above.
func (f fixture) tasksOwner(t *testing.T) uuid.UUID {
	t.Helper()
	p, err := f.q.GetProjectByID(context.Background(), f.project.ID)
	require.NoError(t, err)
	return p.OwnerUserID
}

func TestHandleImport_RunTwice_NoDuplicateTasks(t *testing.T) {
	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{
		issue(1, "first bug", "opened", time.Now(), "bug"),
		issue(2, "second issue", "opened", time.Now()),
	}}
	f := newFixture(t, fake)

	first := f.runImport(t, true)
	assert.EqualValues(t, 2, first.IssuesCreated)
	require.Len(t, f.tasksInProject(t), 2)

	second := f.runImport(t, true)
	assert.EqualValues(t, 0, second.IssuesCreated, "re-running the import must not create duplicate tasks")
	assert.EqualValues(t, 2, second.IssuesSeen)
	assert.Len(t, f.tasksInProject(t), 2, "task count must stay the same across a repeated import")
}

func TestHandleImport_PaginatesAllPages(t *testing.T) {
	fake := &gitlab.FakeClient{IssuesPages: [][]gitlab.Issue{
		{issue(1, "page one issue", "opened", time.Now())},
		{issue(2, "page two issue", "opened", time.Now())},
	}}
	f := newFixture(t, fake)

	run := f.runImport(t, true)

	assert.Equal(t, "succeeded", run.Status)
	assert.EqualValues(t, 2, run.IssuesSeen)
	assert.EqualValues(t, 2, run.IssuesCreated)
	require.Len(t, f.tasksInProject(t), 2)

	var pagesRequested []int
	for _, call := range fake.CallLog {
		if call.Method != "ListIssues" {
			continue
		}
		opts, ok := call.Args[2].(gitlab.ListIssuesOptions)
		require.True(t, ok)
		pagesRequested = append(pagesRequested, opts.Page)
	}
	assert.Equal(t, []int{1, 2}, pagesRequested, "the walk must fetch both pages in order")
}

func TestHandleImport_StaleIssue_DoesNotOverwriteNewerLocalState(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()

	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{issue(1, "original title", "opened", newer)}}
	f := newFixture(t, fake)
	f.runImport(t, true)

	tasks := f.tasksInProject(t)
	require.Len(t, tasks, 1)
	original := tasks[0]

	// A resync delivering a stale (older) updated_at for the same issue must
	// not overwrite the task, reusing the same stale guard
	// internal/webhookapply's inbound pipeline uses.
	fake.Issues = []gitlab.Issue{issue(1, "stale title", "opened", older)}
	f.runResync(t, true)

	tasks = f.tasksInProject(t)
	require.Len(t, tasks, 1)
	assert.Equal(t, original.Title, tasks[0].Title, "a stale delivery must not overwrite the task's title")
}

// Progress sync on issue close (issue #202) must also work through the
// periodic resync path, not just inbound webhooks.
func TestHandleResync_ProgressSyncOnClose_SettingOff_LeavesProgressAlone(t *testing.T) {
	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{issue(1, "task", "opened", time.Now())}}
	f := newFixture(t, fake)
	f.runImport(t, true)

	fake.Issues = []gitlab.Issue{issue(1, "task", "closed", time.Now().Add(time.Hour))}
	f.runResync(t, true)

	tasks := f.tasksInProject(t)
	require.Len(t, tasks, 1)
	assert.Equal(t, "closed", tasks[0].Status)
	assert.Equal(t, "not_started", tasks[0].Progress)

	events, err := f.q.ListTaskProgressEventsByTask(context.Background(), tasks[0].ID)
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestHandleResync_ProgressSyncOnClose_SettingOn_MovesProgressToDone(t *testing.T) {
	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{issue(1, "task", "opened", time.Now())}}
	f := newFixture(t, fake)
	f.runImport(t, true)

	_, err := f.q.UpsertProgressSyncSettings(context.Background(), db.UpsertProgressSyncSettingsParams{ProjectID: f.project.ID, Enabled: true})
	require.NoError(t, err)

	fake.Issues = []gitlab.Issue{issue(1, "task", "closed", time.Now().Add(time.Hour))}
	f.runResync(t, true)

	tasks := f.tasksInProject(t)
	require.Len(t, tasks, 1)
	assert.Equal(t, "closed", tasks[0].Status)
	assert.Equal(t, "done", tasks[0].Progress)

	events, err := f.q.ListTaskProgressEventsByTask(context.Background(), tasks[0].ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "gitlab", events[0].ActorKind)
	assert.Equal(t, "done", events[0].ToProgress)
}

// A second resync run over an already-closed issue (import/resync's own
// idempotent re-run guarantee) must never append a second progress event.
func TestHandleResync_ProgressSyncOnClose_RunTwice_NoDuplicateEvent(t *testing.T) {
	fake := &gitlab.FakeClient{Issues: []gitlab.Issue{issue(1, "task", "opened", time.Now())}}
	f := newFixture(t, fake)
	f.runImport(t, true)

	_, err := f.q.UpsertProgressSyncSettings(context.Background(), db.UpsertProgressSyncSettingsParams{ProjectID: f.project.ID, Enabled: true})
	require.NoError(t, err)

	fake.Issues = []gitlab.Issue{issue(1, "task", "closed", time.Now().Add(time.Hour))}
	f.runResync(t, true)
	f.runResync(t, true)

	tasks := f.tasksInProject(t)
	require.Len(t, tasks, 1)

	events, err := f.q.ListTaskProgressEventsByTask(context.Background(), tasks[0].ID)
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestService_TriggerResync_ConflictWhenRunAlreadyInProgress(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{})
	ctx := context.Background()
	owner := f.tasksOwner(t)

	_, err := f.svc.TriggerResync(ctx, owner, f.link.ID, false)
	require.NoError(t, err)

	_, err = f.svc.TriggerResync(ctx, owner, f.link.ID, false)
	assert.ErrorIs(t, err, projectsync.ErrRunInProgress)
}

func TestService_TriggerResync_NotFoundForForeignLink(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{})
	stranger := f.q.SeedUser("stranger", "stranger@example.com")

	_, err := f.svc.TriggerResync(context.Background(), stranger.ID, f.link.ID, false)
	assert.ErrorIs(t, err, projectsync.ErrNotFound)
}

func TestService_ListRuns_NewestFirst(t *testing.T) {
	f := newFixture(t, &gitlab.FakeClient{})
	owner := f.tasksOwner(t)
	ctx := context.Background()

	first := f.runImport(t, true)
	_, err := f.q.CreateGitlabSyncRun(ctx, db.CreateGitlabSyncRunParams{LinkedGitlabProjectID: f.link.ID, Kind: projectsync.RunKindManualResync})
	require.NoError(t, err)

	runs, err := f.svc.ListRuns(ctx, owner, f.link.ID)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "running", runs[0].Status, "the most recently created run is listed first")
	assert.Equal(t, first.ID, runs[1].ID)
}
