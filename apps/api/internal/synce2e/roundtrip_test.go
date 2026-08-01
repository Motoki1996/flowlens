//go:build integration

// Package synce2e is the bidirectional GitLab issue sync regression suite
// (issue #29): end-to-end scenarios driven against a real PostgreSQL and a
// gitlab.FakeClient, exercising internal/task, internal/issuesync (outbound),
// internal/webhookevent + internal/webhookapply (inbound) and
// internal/projectsync (initial import / manual resync) together, the way
// they actually run in production. Each package's own integration_test.go
// keeps proving out the SQL/concurrency guarantee only it owns (see
// docs/testing.md); this suite instead proves the seams between those
// packages hold up across a full round trip, in particular that FlowLens
// never echoes its own pushes back to GitLab (loop prevention).
// Run with: make test-integration (requires DATABASE_URL and migrations
// already applied).
package synce2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/crypto"
	"github.com/flowlens/api/internal/database"
	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectsync"
	syncpkg "github.com/flowlens/api/internal/sync"
	"github.com/flowlens/api/internal/task"
	"github.com/flowlens/api/internal/webhookapply"
	"github.com/flowlens/api/internal/webhookevent"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crossPackageLockKey serializes this package's integration tests against
// every other package that also drives real claim-and-execute workers over
// sync_jobs/webhook_events (internal/sync, internal/webhookapply): those
// tables have no per-package scoping, so `go test ./...`'s default
// parallelism across packages otherwise lets one package's worker claim rows
// enqueued by another's test via the unscoped FOR UPDATE SKIP LOCKED claim —
// in particular this suite's own drainOutbox/drainInbox racing against
// another package's own background workers over the same tables. Must match
// the key used by those other packages' TestMain.
const crossPackageLockKey = 727208135

// TestMain holds a session-level Postgres advisory lock for this package's
// whole test run, so it and every other integration-tagged package sharing
// crossPackageLockKey run one at a time against the real database instead of
// racing. A session-level lock is released automatically if the process
// exits or the connection drops for any reason, so there is no deadlock risk
// from a crash or a CI-enforced timeout skipping the deferred unlock.
func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "synce2e: TestMain: connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", crossPackageLockKey); err != nil {
		fmt.Fprintf(os.Stderr, "synce2e: TestMain: acquire lock: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", crossPackageLockKey); err != nil {
		fmt.Fprintf(os.Stderr, "synce2e: TestMain: release lock: %v\n", err)
	}
	os.Exit(code)
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	pool, err := database.Connect(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.New([]byte("01234567890123456789012345678901"[:32]))
	require.NoError(t, err)
	return c
}

// env bundles one app project with its linked GitLab project, and the real
// services that drive both sync directions over it, sharing one FakeClient
// every outbound call goes through. One env per test, so each of the 7
// scenarios is fully independent per docs/testing.md's "keep integration
// tests independent".
type env struct {
	pool      *pgxpool.Pool
	q         db.Querier
	ownerID   uuid.UUID
	projectID uuid.UUID
	link      db.LinkedGitlabProject
	fake      *gitlab.FakeClient
	tasks     *task.Service
	webhooks  *webhookevent.Service
	apply     *webhookapply.Service
	projSync  *projectsync.Service
	worker    *syncpkg.Worker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	q := db.New(pool)
	cipher := testCipher(t)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		Username:     "synce2e-" + suffix,
		Email:        "synce2e-" + suffix + "@example.com",
		DisplayName:  "Sync E2E Test",
		PasswordHash: "bcrypt-hash",
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
		assert.NoError(t, err)
	})

	appProject, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerUserID: user.ID, Name: "synce2e-project"})
	require.NoError(t, err)

	encryptedToken, err := cipher.Encrypt("glpat-test-token")
	require.NoError(t, err)
	conn, err := q.UpsertGitlabConnection(ctx, db.UpsertGitlabConnectionParams{
		ProjectID:      appProject.ID,
		BaseUrl:        "https://gitlab.example.com",
		EncryptedToken: encryptedToken,
	})
	require.NoError(t, err)

	// The first linked project on a connection is automatically its default
	// (see linked_gitlab_projects.sql), which is what makes
	// internal/task.Service.Create push a newly created task straight away.
	link, err := q.CreateLinkedGitlabProject(ctx, db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          "all",
		SyncLabels:         []string{},
	})
	require.NoError(t, err)

	fake := &gitlab.FakeClient{}
	factory := func(string) gitlab.Client { return fake }
	txRunner := database.NewTxRunner(pool)

	projects := project.NewService(q)
	backlogs := backlog.NewService(q, projects)
	tasks := task.NewService(q, txRunner, projects, backlogs)

	issuesyncSvc := issuesync.NewService(q, cipher, factory)
	projSyncSvc := projectsync.NewService(q, txRunner, cipher, factory)

	registry := syncpkg.NewRegistry()
	registry.Register(issuesync.KindIssueCreate, issuesyncSvc.HandleIssueCreate)
	registry.Register(issuesync.KindIssueUpdate, issuesyncSvc.HandleIssueUpdate)
	registry.Register(issuesync.KindIssueClose, issuesyncSvc.HandleIssueClose)
	registry.Register(issuesync.KindIssueReopen, issuesyncSvc.HandleIssueReopen)
	registry.Register(projectsync.KindProjectImport, projSyncSvc.HandleImport)
	registry.Register(projectsync.KindProjectResync, projSyncSvc.HandleResync)

	return &env{
		pool:      pool,
		q:         q,
		ownerID:   user.ID,
		projectID: appProject.ID,
		link:      link,
		fake:      fake,
		tasks:     tasks,
		webhooks:  webhookevent.NewService(q, cipher),
		apply:     webhookapply.NewService(txRunner),
		projSync:  projSyncSvc,
		worker:    syncpkg.NewWorker(q, registry),
	}
}

// drainOutbox runs the sync_jobs worker until no pending job remains, the
// deterministic alternative to waiting on Worker.Run's poll ticker (see
// internal/sync.Worker.Poll's doc comment).
func (e *env) drainOutbox(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		n, err := e.worker.Poll(ctx)
		require.NoError(t, err)
		if n == 0 {
			return
		}
	}
	t.Fatal("drainOutbox: sync_jobs queue never drained")
}

// drainInbox applies every pending webhook_events row, the same loop
// internal/webhookapply.Worker.Run drives around ProcessNext.
func (e *env) drainInbox(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		claimed, err := e.apply.ProcessNext(ctx)
		require.NoError(t, err)
		if !claimed {
			return
		}
	}
	t.Fatal("drainInbox: webhook_events queue never drained")
}

// issueEvent describes one inbound GitLab "Issue Hook" delivery.
type issueEvent struct {
	ID          int64
	IID         int64
	Title       string
	Description string
	State       string // "opened" | "closed"
	Labels      []string
	AssigneeID  int64
	UpdatedAt   time.Time
}

func issueHookPayload(o issueEvent) []byte {
	type label struct {
		Title string `json:"title"`
	}
	type assignee struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	body := struct {
		ObjectKind       string `json:"object_kind"`
		ObjectAttributes struct {
			ID          int64  `json:"id"`
			IID         int64  `json:"iid"`
			Title       string `json:"title"`
			Description string `json:"description"`
			State       string `json:"state"`
			DueDate     string `json:"due_date"`
			UpdatedAt   string `json:"updated_at"`
			URL         string `json:"url"`
		} `json:"object_attributes"`
		Labels    []label    `json:"labels"`
		Assignees []assignee `json:"assignees"`
	}{ObjectKind: "issue"}

	body.ObjectAttributes.ID = o.ID
	body.ObjectAttributes.IID = o.IID
	body.ObjectAttributes.Title = o.Title
	body.ObjectAttributes.Description = o.Description
	body.ObjectAttributes.State = o.State
	// RFC3339Nano, not RFC3339: GitLab's real payloads carry sub-second
	// precision, and internal/webhookapply's parseGitlabTime can parse it
	// (Go's time.Parse accepts an optional fractional-second field after the
	// seconds even when the layout doesn't specify one). Formatting with
	// plain RFC3339 would truncate to whole seconds, so a same-instant echo
	// (see TestRoundTrip_AppUpdateEcho_IsIgnored_RealPostgres) would come
	// back rounded down and read as strictly before the stored
	// gitlab_updated_at baseline — tripping the stale guard instead of the
	// echo guard.
	body.ObjectAttributes.UpdatedAt = o.UpdatedAt.UTC().Format(time.RFC3339Nano)
	body.ObjectAttributes.URL = fmt.Sprintf("https://gitlab.example.com/group/demo/-/issues/%d", o.IID)
	for _, l := range o.Labels {
		body.Labels = append(body.Labels, label{Title: l})
	}
	if o.AssigneeID != 0 {
		body.Assignees = append(body.Assignees, assignee{ID: o.AssigneeID, Username: "gitlab-user"})
	}

	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

// recordWebhookEvent verifies and records o as a "pending" webhook_events
// row through the real internal/webhookevent.Service, the same path
// internal/http's receiver uses for a genuine GitLab delivery.
func (e *env) recordWebhookEvent(t *testing.T, deliveryUUID string, o issueEvent) {
	t.Helper()
	err := e.webhooks.Record(context.Background(), webhookevent.RecordParams{
		LinkedGitlabProjectID: e.link.ID,
		DeliveryUUID:          deliveryUUID,
		EventName:             webhookevent.SupportedEventHeader,
		ObjectKind:            "issue",
		GitlabIssueIID:        o.IID,
		Payload:               issueHookPayload(o),
		GitlabUpdatedAt:       o.UpdatedAt,
		Status:                webhookevent.StatusPending,
	})
	require.NoError(t, err)
}

func (e *env) webhookEventStatus(t *testing.T, deliveryUUID string) (status, skipReason string) {
	t.Helper()
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT status, skip_reason FROM webhook_events WHERE linked_gitlab_project_id = $1 AND delivery_uuid = $2`,
		e.link.ID, deliveryUUID,
	).Scan(&status, &skipReason))
	return status, skipReason
}

func (e *env) countTaskGitlabLinks(t *testing.T, iid int64) int {
	t.Helper()
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM task_gitlab_links WHERE linked_gitlab_project_id = $1 AND gitlab_issue_iid = $2`,
		e.link.ID, iid,
	).Scan(&n))
	return n
}

// callMethods extracts just the ordered method names from a FakeClient's
// call history, so a test can assert what was (or wasn't) called — and in
// what order — without repeating the boilerplate of walking gitlab.Call.Args
// itself. This is the round trip suite's assertion helper for FakeGitLab's
// call history, used by every scenario that checks outbound behaviour.
func callMethods(callLog []gitlab.Call) []string {
	methods := make([]string, len(callLog))
	for i, c := range callLog {
		methods[i] = c.Method
	}
	return methods
}

// updateIssuePayloads extracts every UpdateIssue call's payload from a
// FakeClient's call history, in call order.
func updateIssuePayloads(callLog []gitlab.Call) []gitlab.UpdateIssuePayload {
	var payloads []gitlab.UpdateIssuePayload
	for _, c := range callLog {
		if c.Method != "UpdateIssue" {
			continue
		}
		if p, ok := c.Args[3].(gitlab.UpdateIssuePayload); ok {
			payloads = append(payloads, p)
		}
	}
	return payloads
}

// Scenario 1: the app creates a task; the outbox worker pushes it to GitLab
// and records the link.
func TestRoundTrip_TaskCreate_PushesIssueAndLinksTask_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	e.fake.Issue = &gitlab.Issue{
		ID: 9001, IID: 7,
		WebURL:    "https://gitlab.example.com/group/demo/-/issues/7",
		UpdatedAt: time.Now().UTC(),
	}

	tsk, err := e.tasks.Create(ctx, e.ownerID, e.projectID, task.CreateParams{
		Title:       "Fix login bug",
		Description: "Users can't log in",
		Labels:      []string{"bug"},
	})
	require.NoError(t, err)

	e.drainOutbox(t)

	assert.Equal(t, []string{"CreateIssue"}, callMethods(e.fake.CallLog), "creating a linked task must push exactly one CreateIssue call")
	assert.Equal(t, e.link.GitlabProjectID, e.fake.CallLog[0].Args[1])
	sent, ok := e.fake.CallLog[0].Args[2].(gitlab.IssuePayload)
	require.True(t, ok)
	assert.Equal(t, "Fix login bug", sent.Title)
	assert.Equal(t, []string{"bug"}, sent.Labels)

	link, err := e.q.GetTaskGitlabLinkByTaskID(ctx, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(9001), link.GitlabIssueID)
	assert.Equal(t, int64(7), link.GitlabIssueIid)
	assert.Equal(t, "synced", link.SyncStatus)
}

// Scenario 2: GitLab reports an issue update via webhook; applying it writes
// the task's mirrored fields but must never call back out to GitLab — the
// structural half of loop prevention (internal/webhookapply never imports
// internal/sync).
func TestRoundTrip_InboundUpdate_AppliesToTask_AndPushesNothingBack_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	base := time.Now().UTC()
	e.fake.Issue = &gitlab.Issue{ID: 9002, IID: 11, WebURL: "https://gitlab.example.com/group/demo/-/issues/11", UpdatedAt: base}

	tsk, err := e.tasks.Create(ctx, e.ownerID, e.projectID, task.CreateParams{Title: "Original title", Description: "orig"})
	require.NoError(t, err)
	e.drainOutbox(t)
	callsBefore := len(e.fake.CallLog)

	e.recordWebhookEvent(t, "delivery-1", issueEvent{
		ID: 9002, IID: 11, Title: "Updated on GitLab", Description: "changed on gitlab side",
		State: "opened", UpdatedAt: base.Add(time.Minute),
	})
	e.drainInbox(t)

	got, err := e.tasks.Get(ctx, e.ownerID, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated on GitLab", got.Title)
	assert.Equal(t, "changed on gitlab side", got.Description)

	assert.Len(t, e.fake.CallLog, callsBefore, "applying an inbound webhook must never call back out to GitLab (loop prevention)")

	status, _ := e.webhookEventStatus(t, "delivery-1")
	assert.Equal(t, "processed", status)
}

// Scenario 3: GitLab redelivers the same webhook (same delivery UUID) twice,
// for an issue FlowLens has never seen before. The UNIQUE
// (linked_gitlab_project_id, delivery_uuid) constraint makes the second
// Record call a no-op, so only one task is ever created from it.
func TestRoundTrip_DuplicateWebhookDelivery_CreatesOneTaskAndOneEvent_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	now := time.Now().UTC()
	evt := issueEvent{ID: 9100, IID: 21, Title: "New from GitLab", Description: "d", State: "opened", UpdatedAt: now}

	e.recordWebhookEvent(t, "dup-delivery", evt)
	e.recordWebhookEvent(t, "dup-delivery", evt)

	var eventCount int
	require.NoError(t, e.pool.QueryRow(ctx,
		`SELECT count(*) FROM webhook_events WHERE linked_gitlab_project_id = $1 AND delivery_uuid = $2`,
		e.link.ID, "dup-delivery",
	).Scan(&eventCount))
	assert.Equal(t, 1, eventCount, "the same delivery UUID recorded twice must not create a second row")

	e.drainInbox(t)

	assert.Equal(t, 1, e.countTaskGitlabLinks(t, evt.IID))
}

// Scenario 4: two webhooks for the same known issue arrive out of order —
// the newer content first, then a reordered redelivery of older content.
// The stale guard (updated_at strictly before the last applied value) must
// keep the newer content from being clobbered.
func TestRoundTrip_OutOfOrderWebhooks_NewerContentWins_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	base := time.Now().UTC()
	e.fake.Issue = &gitlab.Issue{ID: 9200, IID: 31, WebURL: "https://gitlab.example.com/group/demo/-/issues/31", UpdatedAt: base}

	tsk, err := e.tasks.Create(ctx, e.ownerID, e.projectID, task.CreateParams{Title: "Original", Description: "orig"})
	require.NoError(t, err)
	e.drainOutbox(t)

	e.recordWebhookEvent(t, "newer", issueEvent{
		ID: 9200, IID: 31, Title: "New content", Description: "new", State: "opened",
		UpdatedAt: base.Add(20 * time.Second),
	})
	e.drainInbox(t)

	got, err := e.tasks.Get(ctx, e.ownerID, tsk.ID)
	require.NoError(t, err)
	require.Equal(t, "New content", got.Title)

	// A reordered redelivery of the older content arrives second.
	e.recordWebhookEvent(t, "older", issueEvent{
		ID: 9200, IID: 31, Title: "Stale content", Description: "stale", State: "opened",
		UpdatedAt: base.Add(10 * time.Second),
	})
	e.drainInbox(t)

	got, err = e.tasks.Get(ctx, e.ownerID, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, "New content", got.Title, "a reordered, older-updated_at delivery must not overwrite newer content")

	status, skipReason := e.webhookEventStatus(t, "older")
	assert.Equal(t, "skipped", status)
	assert.Equal(t, "stale", skipReason)
}

// Scenario 5: the app edits a task, the outbox pushes the change to GitLab,
// and GitLab's own webhook echoing that exact change back must be
// recognized and ignored — the content-fingerprint half of loop prevention.
func TestRoundTrip_AppUpdateEcho_IsIgnored_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	base := time.Now().UTC()
	e.fake.Issue = &gitlab.Issue{ID: 9300, IID: 41, WebURL: "https://gitlab.example.com/group/demo/-/issues/41", UpdatedAt: base}

	tsk, err := e.tasks.Create(ctx, e.ownerID, e.projectID, task.CreateParams{
		Title: "Original", Description: "orig", Labels: []string{"bug"},
	})
	require.NoError(t, err)
	e.drainOutbox(t)

	pushedAt := base.Add(time.Minute)
	e.fake.Issue = &gitlab.Issue{ID: 9300, IID: 41, WebURL: "https://gitlab.example.com/group/demo/-/issues/41", UpdatedAt: pushedAt}

	_, err = e.tasks.Update(ctx, e.ownerID, tsk.ID, task.UpdateParams{
		Title: task.Present("Edited from app"), Description: task.Present("edited"), Labels: task.Present([]string{"bug"}),
	})
	require.NoError(t, err)
	e.drainOutbox(t)
	callsBefore := len(e.fake.CallLog)

	// GitLab echoes FlowLens's own push back as a webhook: identical
	// content, the same updated_at GitLab assigned when it applied the push.
	e.recordWebhookEvent(t, "echo-delivery", issueEvent{
		ID: 9300, IID: 41, Title: "Edited from app", Description: "edited", State: "opened",
		Labels: []string{"bug"}, UpdatedAt: pushedAt,
	})
	e.drainInbox(t)

	assert.Len(t, e.fake.CallLog, callsBefore, "applying the echo must never call back out to GitLab")

	status, skipReason := e.webhookEventStatus(t, "echo-delivery")
	assert.Equal(t, "skipped", status)
	assert.Equal(t, "echo", skipReason)

	got, err := e.tasks.Get(ctx, e.ownerID, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, "Edited from app", got.Title, "the echo being skipped must leave the app's own edit intact")
}

// Scenario 6: an initial import picks up an issue that already existed on
// GitLab; a later manual resync over the same, unchanged issue must not
// create a duplicate task.
func TestRoundTrip_InitialImportThenManualResync_NoDuplicateTasks_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	updatedAt := time.Now().UTC()
	e.fake.Issues = []gitlab.Issue{{
		ID: 9400, IID: 51, Title: "Pre-existing GitLab issue", Description: "already on gitlab",
		State: "opened", UpdatedAt: updatedAt, WebURL: "https://gitlab.example.com/group/demo/-/issues/51",
	}}

	run1, err := e.q.CreateGitlabSyncRun(ctx, db.CreateGitlabSyncRunParams{
		LinkedGitlabProjectID: e.link.ID, Kind: projectsync.RunKindInitialImport,
	})
	require.NoError(t, err)
	payload1, err := json.Marshal(projectsync.RunPayload{SyncRunID: run1.ID, Full: true})
	require.NoError(t, err)
	require.NoError(t, e.projSync.HandleImport(ctx, db.SyncJob{Payload: payload1}))

	assert.Equal(t, 1, e.countTaskGitlabLinks(t, 51), "the initial import must create exactly one task for the pre-existing issue")

	run2, err := e.q.CreateGitlabSyncRun(ctx, db.CreateGitlabSyncRunParams{
		LinkedGitlabProjectID: e.link.ID, Kind: projectsync.RunKindManualResync,
	})
	require.NoError(t, err)
	payload2, err := json.Marshal(projectsync.RunPayload{SyncRunID: run2.ID, Full: true})
	require.NoError(t, err)
	require.NoError(t, e.projSync.HandleResync(ctx, db.SyncJob{Payload: payload2}))

	assert.Equal(t, 1, e.countTaskGitlabLinks(t, 51), "re-running the sync over the same, unchanged issue must not create a duplicate task")

	var run1Status, run2Status string
	require.NoError(t, e.pool.QueryRow(ctx, "SELECT status FROM gitlab_sync_runs WHERE id = $1", run1.ID).Scan(&run1Status))
	require.NoError(t, e.pool.QueryRow(ctx, "SELECT status FROM gitlab_sync_runs WHERE id = $1", run2.ID).Scan(&run2Status))
	assert.Equal(t, "succeeded", run1Status)
	assert.Equal(t, "succeeded", run2Status)
}

// Scenario 7: GitLab reports the issue closed via webhook; the app then
// reopens the task, which must push state_event=reopen back to GitLab.
func TestRoundTrip_GitlabClose_ThenAppReopen_PushesReopenToGitlab_RealPostgres(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	base := time.Now().UTC()
	e.fake.Issue = &gitlab.Issue{ID: 9500, IID: 61, WebURL: "https://gitlab.example.com/group/demo/-/issues/61", UpdatedAt: base}

	tsk, err := e.tasks.Create(ctx, e.ownerID, e.projectID, task.CreateParams{Title: "Investigate outage"})
	require.NoError(t, err)
	e.drainOutbox(t)

	e.recordWebhookEvent(t, "closed-on-gitlab", issueEvent{
		ID: 9500, IID: 61, Title: "Investigate outage", State: "closed", UpdatedAt: base.Add(time.Minute),
	})
	e.drainInbox(t)

	got, err := e.tasks.Get(ctx, e.ownerID, tsk.ID)
	require.NoError(t, err)
	require.Equal(t, task.StatusClosed, got.Status, "the GitLab-side close must be applied to the task")

	reopened, err := e.tasks.Reopen(ctx, e.ownerID, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, reopened.Status)

	e.drainOutbox(t)

	var sawReopen bool
	for _, payload := range updateIssuePayloads(e.fake.CallLog) {
		if payload.StateEvent == "reopen" {
			sawReopen = true
		}
	}
	assert.True(t, sawReopen, "reopening the task must push state_event=reopen to GitLab")
}
