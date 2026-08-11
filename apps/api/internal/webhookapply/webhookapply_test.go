package webhookapply_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flowlens/api/internal/database/db"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/gitlab"
	"github.com/flowlens/api/internal/linkedproject"
	"github.com/flowlens/api/internal/task"
	"github.com/flowlens/api/internal/webhookapply"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture bundles a webhookapply Service with an in-memory querier and a
// linked GitLab project already wired to a real project/owner chain, so
// tests that create a new unclassified task (the "unknown issue" path) can
// resolve an owner the same way production does.
type fixture struct {
	svc       *webhookapply.Service
	q         *dbtest.FakeQuerier
	ownerID   uuid.UUID
	projectID uuid.UUID
	link      db.LinkedGitlabProject
}

func newFixture(t *testing.T, scope string, syncLabels []string) fixture {
	t.Helper()
	q := dbtest.New()
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	conn := q.SeedGitlabConnection(p.ID, []byte("encrypted-token"))
	link, err := q.CreateLinkedGitlabProject(context.Background(), db.CreateLinkedGitlabProjectParams{
		GitlabConnectionID: conn.ID,
		GitlabProjectID:    100,
		PathWithNamespace:  "group/demo",
		Name:               "demo",
		WebUrl:             "https://gitlab.example.com/group/demo",
		SyncScope:          scope,
		SyncLabels:         syncLabels,
	})
	require.NoError(t, err)

	svc := webhookapply.NewService(dbtest.FakeTxRunner{Q: q})
	return fixture{svc: svc, q: q, ownerID: owner.ID, projectID: p.ID, link: link}
}

// issuePayloadOpts describes a GitLab "Issue Hook" delivery for
// issuePayload to marshal. UpdatedAt defaults to time.Now() when zero, and
// State defaults to "opened".
type issuePayloadOpts struct {
	ID               int64
	IID              int64
	Title            string
	Description      string
	State            string
	Labels           []string
	AssigneeID       int64
	AssigneeUsername string
	DueDate          string
	UpdatedAt        time.Time
	URL              string
}

func issuePayload(o issuePayloadOpts) []byte {
	if o.State == "" {
		o.State = "opened"
	}
	if o.UpdatedAt.IsZero() {
		o.UpdatedAt = time.Now()
	}
	if o.ID == 0 {
		o.ID = o.IID + 1000
	}

	labels := make([]map[string]string, 0, len(o.Labels))
	for _, l := range o.Labels {
		labels = append(labels, map[string]string{"title": l})
	}
	assignees := []map[string]any{}
	if o.AssigneeID != 0 {
		assignees = append(assignees, map[string]any{"id": o.AssigneeID, "username": o.AssigneeUsername})
	}

	body := map[string]any{
		"object_kind": "issue",
		"object_attributes": map[string]any{
			"id":          o.ID,
			"iid":         o.IID,
			"title":       o.Title,
			"description": o.Description,
			"state":       o.State,
			"due_date":    o.DueDate,
			"updated_at":  o.UpdatedAt.UTC().Format(time.RFC3339),
			"url":         o.URL,
		},
		"labels":    labels,
		"assignees": assignees,
	}
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

func TestProcessNext_NoPendingEvents_ReturnsFalse(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)

	claimed, err := f.svc.ProcessNext(context.Background())
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestProcessNext_UnknownIssue_CreatesUnclassifiedTask(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	event := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 42, Title: "New from GitLab", Description: "body",
		Labels: []string{"bug"}, DueDate: "2026-08-01",
		AssigneeID: 7, AssigneeUsername: "alice",
		URL: "https://gitlab.example.com/group/demo/-/issues/42",
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status)

	link, err := f.q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
		LinkedGitlabProjectID: f.link.ID, GitlabIssueIid: 42,
	})
	require.NoError(t, err)

	tsk, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: link.TaskID, OwnerUserID: f.ownerID})
	require.NoError(t, err)
	assert.Equal(t, "New from GitLab", tsk.Title)
	assert.Equal(t, "body", tsk.Description)
	assert.Equal(t, []string{"bug"}, tsk.Labels)
	assert.False(t, tsk.BacklogID.Valid, "an unknown issue must create an unclassified (backlog_id NULL) task")
	assert.Equal(t, task.StatusOpen, tsk.Status)
	assert.EqualValues(t, 7, tsk.AssigneeGitlabUserID.Int64)
}

func TestProcessNext_OutOfScope_SkipsAndCreatesNoTask(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeLabels, []string{"sync-me"})
	ctx := context.Background()

	event := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 1, Title: "Not in scope", Labels: []string{"other-label"},
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "skipped", got.Status)
	assert.Equal(t, webhookapply.SkipReasonOutOfScope, got.SkipReason)

	_, err = f.q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
		LinkedGitlabProjectID: f.link.ID, GitlabIssueIid: 1,
	})
	assert.Error(t, err, "an out-of-scope issue must never create a task or a link")
}

func TestProcessNext_KnownIssue_UpdatesExistingTask(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Old title")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 99)

	event := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 99, Title: "New title", Description: "updated body",
		Labels: []string{"urgent"}, State: "closed",
		AssigneeID: 5, AssigneeUsername: "bob",
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status)

	updated, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: tsk.ID, OwnerUserID: f.ownerID})
	require.NoError(t, err)
	assert.Equal(t, "New title", updated.Title)
	assert.Equal(t, "updated body", updated.Description)
	assert.Equal(t, []string{"urgent"}, updated.Labels)
	assert.Equal(t, task.StatusClosed, updated.Status)
	assert.True(t, updated.ClosedAt.Valid)
	assert.EqualValues(t, 5, updated.AssigneeGitlabUserID.Int64)
}

// Guard 2 ("stale"): an event whose payload updated_at is strictly before
// task_gitlab_links.gitlab_updated_at must be skipped, never applied — this
// is what makes ordering depend on GitLab's own timestamps rather than
// arrival order, so a reordered redelivery can never clobber a newer state.
// A tied timestamp is not stale (see
// TestProcessNext_ConcurrentEventsForSameNewIssue_OnlyOneTaskCreated_RealPostgres):
// it falls through to the echo guard, or is applied if its content differs.
func TestProcessNext_StaleEvent_SkippedAndTaskUnchanged(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Current title")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 7)
	newer := time.Now()
	_, err := f.q.MarkTaskGitlabLinkSyncedForTask(ctx, db.MarkTaskGitlabLinkSyncedForTaskParams{
		TaskID:                tsk.ID,
		GitlabUpdatedAt:       pgtype.Timestamptz{Time: newer, Valid: true},
		LastPushedFingerprint: "irrelevant-fingerprint",
	})
	require.NoError(t, err)

	older := newer.Add(-time.Hour)
	event := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 7, Title: "Stale title (arrived out of order)", UpdatedAt: older,
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "skipped", got.Status)
	assert.Equal(t, webhookapply.SkipReasonStale, got.SkipReason)

	unchanged, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: tsk.ID, OwnerUserID: f.ownerID})
	require.NoError(t, err)
	assert.Equal(t, "Current title", unchanged.Title, "a stale event must never overwrite a newer applied state")
}

// Guard 3 ("echo"): a delivery whose content fingerprint matches
// task_gitlab_links.last_pushed_fingerprint is FlowLens's own outbound push
// coming back, and must be skipped even though its updated_at is newer (so
// the stale guard alone does not catch it).
func TestProcessNext_EchoEvent_SkippedAndTaskUnchanged(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Current title")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 8)

	baseline := time.Now().Add(-time.Hour)
	fp := gitlab.Fingerprint("Echoed title", "echoed body", []string{"x"}, nil, nil)
	_, err := f.q.MarkTaskGitlabLinkSyncedForTask(ctx, db.MarkTaskGitlabLinkSyncedForTaskParams{
		TaskID:                tsk.ID,
		GitlabUpdatedAt:       pgtype.Timestamptz{Time: baseline, Valid: true},
		LastPushedFingerprint: fp,
	})
	require.NoError(t, err)

	event := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 8, Title: "Echoed title", Description: "echoed body", Labels: []string{"x"},
		UpdatedAt: baseline.Add(time.Minute),
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "skipped", got.Status)
	assert.Equal(t, webhookapply.SkipReasonEcho, got.SkipReason)

	unchanged, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: tsk.ID, OwnerUserID: f.ownerID})
	require.NoError(t, err)
	assert.Equal(t, "Current title", unchanged.Title, "FlowLens's own push echoing back must never re-apply")
}

// A close/reopen push never touches last_pushed_fingerprint (see
// internal/issuesync.handleStateChange), so a genuine external close whose
// title/description/labels still match the last content push has the same
// fingerprint as a true echo. The echo guard must tell them apart by status,
// not skip the close.
func TestProcessNext_GitlabCloseWithUnchangedContent_NotTreatedAsEcho(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Investigate outage")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 61)

	baseline := time.Now().Add(-time.Hour)
	fp := gitlab.Fingerprint("Investigate outage", "", []string{}, nil, nil)
	_, err := f.q.MarkTaskGitlabLinkSyncedForTask(ctx, db.MarkTaskGitlabLinkSyncedForTaskParams{
		TaskID:                tsk.ID,
		GitlabUpdatedAt:       pgtype.Timestamptz{Time: baseline, Valid: true},
		LastPushedFingerprint: fp,
	})
	require.NoError(t, err)

	event := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 61, Title: "Investigate outage", State: "closed",
		UpdatedAt: baseline.Add(time.Minute),
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status, "a real GitLab-side close must be applied, not skipped as an echo")

	updated, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: tsk.ID, OwnerUserID: f.ownerID})
	require.NoError(t, err)
	assert.Equal(t, task.StatusClosed, updated.Status)
}

// Guard 1 ("duplicate"): a second delivery for a not-yet-known issue that
// arrives after a first delivery already created and linked a task must
// update that same task, never create a second one — the UNIQUE
// (linked_gitlab_project_id, gitlab_issue_iid) constraint is what makes this
// safe even under real concurrency (verified against real Postgres in
// TestProcessNext_ConcurrentEventsForSameNewIssue_RealPostgres); here it is
// exercised sequentially at the domain layer.
func TestProcessNext_SecondEventForSameNewIssue_UpdatesInsteadOfDuplicating(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	base := time.Now()
	f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 200, Title: "First delivery", UpdatedAt: base,
	}))
	f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
		IID: 200, Title: "Second delivery", UpdatedAt: base.Add(time.Minute),
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	link, err := f.q.GetTaskGitlabLinkByLinkedProjectAndIID(ctx, db.GetTaskGitlabLinkByLinkedProjectAndIIDParams{
		LinkedGitlabProjectID: f.link.ID, GitlabIssueIid: 200,
	})
	require.NoError(t, err)

	got, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: link.TaskID, OwnerUserID: f.ownerID})
	require.NoError(t, err)
	assert.Equal(t, "Second delivery", got.Title, "the second delivery must update the same task")

	tasks, err := f.q.ListTasksByProject(ctx, db.ListTasksByProjectParams{ProjectID: f.projectID})
	require.NoError(t, err)
	assert.Len(t, tasks, 1, "two deliveries for the same new issue must never create two tasks")
}

// The apply path must never enqueue an outbound sync_jobs row — the
// structural half of the loop-prevention guard (docs/plans/issue-sync.md,
// "Inbound"): webhookapply never imports internal/sync, so there is no
// Enqueue call anywhere on this path to begin with.
func TestProcessNext_NeverEnqueuesOutboundSyncJob(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Existing")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 9)
	f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{IID: 9, Title: "Updated by GitLab"}))
	f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{IID: 10, Title: "Brand new issue"}))

	for i := 0; i < 2; i++ {
		claimed, err := f.svc.ProcessNext(ctx)
		require.NoError(t, err)
		require.True(t, claimed)
	}

	assert.Equal(t, 0, f.q.SyncJobCount(), "applying inbound webhook events must never enqueue an outbound sync job")
}

// task_ai_contexts is app-only and must never be touched by an inbound
// apply, even when the mirrored task fields change.
func TestProcessNext_PreservesAIContext(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Old title")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 3)
	aiCtx, err := f.q.UpsertTaskAIContext(ctx, db.UpsertTaskAIContextParams{
		TaskID:             tsk.ID,
		AcceptanceCriteria: "must pass tests",
		AiContext:          "context for the AI agent",
		AllowedScope:       "src/",
		ForbiddenScope:     "infra/",
	})
	require.NoError(t, err)

	f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{IID: 3, Title: "New title from GitLab"}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	after, err := f.q.GetTaskAIContext(ctx, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, aiCtx, after, "webhook apply must never touch task_ai_contexts")
}

// Close/reopen deliveries must map onto tasks.status correctly regardless of
// how many times the issue flips state.
func TestProcessNext_CloseThenReopen_TableDriven(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	tsk := f.q.SeedTask(f.projectID, f.ownerID, "Task")
	f.q.SeedTaskGitlabLink(tsk.ID, f.link.ID, 55)

	steps := []struct {
		name       string
		state      string
		wantStatus string
	}{
		{"update while open", "opened", task.StatusOpen},
		{"close", "closed", task.StatusClosed},
		{"reopen", "opened", task.StatusOpen},
		{"close again", "closed", task.StatusClosed},
	}

	base := time.Now()
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			base = base.Add(time.Minute)
			f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{
				IID: 55, Title: "Task", State: step.state, UpdatedAt: base,
			}))

			claimed, err := f.svc.ProcessNext(ctx)
			require.NoError(t, err)
			require.True(t, claimed)

			got, err := f.q.GetTaskForOwner(ctx, db.GetTaskForOwnerParams{ID: tsk.ID, OwnerUserID: f.ownerID})
			require.NoError(t, err)
			assert.Equal(t, step.wantStatus, got.Status)
			if step.wantStatus == task.StatusClosed {
				assert.True(t, got.ClosedAt.Valid)
			} else {
				assert.False(t, got.ClosedAt.Valid)
			}
		})
	}
}

// The worker processes webhook_events oldest received first.
func TestProcessNext_OldestPendingEventClaimedFirst(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	first := f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{IID: 1, Title: "one"}))
	time.Sleep(2 * time.Millisecond)
	f.q.SeedWebhookEvent(f.link.ID, issuePayload(issuePayloadOpts{IID: 2, Title: "two"}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(first.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status, "the oldest pending event must be claimed first")
}

// seedEvent inserts a webhook_events row of an arbitrary objectKind/eventName
// (SeedWebhookEvent only builds "Issue Hook"/"issue" rows), for the
// merge_request/pipeline dispatch tests below (issue #111).
func (f fixture) seedEvent(t *testing.T, objectKind, eventName string, payload []byte) db.WebhookEvent {
	t.Helper()
	event, err := f.q.CreateWebhookEvent(context.Background(), db.CreateWebhookEventParams{
		LinkedGitlabProjectID: f.link.ID,
		DeliveryUuid:          uuid.NewString(),
		EventName:             eventName,
		ObjectKind:            objectKind,
		Payload:               payload,
		Status:                "pending",
	})
	require.NoError(t, err)
	return event
}

type mrPayloadOpts struct {
	ID           int64
	IID          int64
	Title        string
	Description  string
	State        string
	SourceBranch string
	TargetBranch string
	UpdatedAt    time.Time
	URL          string
}

func mrPayload(o mrPayloadOpts) []byte {
	if o.State == "" {
		o.State = "opened"
	}
	if o.UpdatedAt.IsZero() {
		o.UpdatedAt = time.Now()
	}
	if o.ID == 0 {
		o.ID = o.IID + 1000
	}
	body := map[string]any{
		"object_kind": "merge_request",
		"object_attributes": map[string]any{
			"id":            o.ID,
			"iid":           o.IID,
			"title":         o.Title,
			"description":   o.Description,
			"state":         o.State,
			"source_branch": o.SourceBranch,
			"target_branch": o.TargetBranch,
			"updated_at":    o.UpdatedAt.UTC().Format(time.RFC3339),
			"created_at":    o.UpdatedAt.UTC().Format(time.RFC3339),
			"url":           o.URL,
		},
		"user": map[string]any{"username": "octocat", "avatar_url": ""},
	}
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

func TestProcessNext_UnknownMergeRequest_CreatesRow(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()
	_, err := f.q.CreateRepository(ctx, db.CreateRepositoryParams{LinkedGitlabProjectID: f.link.ID, Name: "demo", FullName: "group/demo"})
	require.NoError(t, err)

	event := f.seedEvent(t, "merge_request", "Merge Request Hook", mrPayload(mrPayloadOpts{
		IID: 5, Title: "Add feature", SourceBranch: "feature-5", TargetBranch: "main",
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(ctx, 1005)
	require.NoError(t, err)
	assert.Equal(t, "Add feature", mr.Title)
	assert.Equal(t, "opened", mr.State)
}

func TestProcessNext_KnownMergeRequest_Updates(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()
	repo, err := f.q.CreateRepository(ctx, db.CreateRepositoryParams{LinkedGitlabProjectID: f.link.ID, Name: "demo", FullName: "group/demo"})
	require.NoError(t, err)
	_, err = f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: repo.ID, GitlabMergeRequestID: 1005, Number: 5,
		Title: "Add feature", State: "opened",
		GitlabUpdatedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	require.NoError(t, err)

	event := f.seedEvent(t, "merge_request", "Merge Request Hook", mrPayload(mrPayloadOpts{
		IID: 5, Title: "Add feature", State: "merged", SourceBranch: "feature-5", TargetBranch: "main",
	}))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status)

	mr, err := f.q.GetMergeRequestByGitlabMergeRequestID(ctx, 1005)
	require.NoError(t, err)
	assert.Equal(t, "merged", mr.State)
}

func TestProcessNext_DuplicateMergeRequestDelivery_IsNoOp(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()
	_, err := f.q.CreateRepository(ctx, db.CreateRepositoryParams{LinkedGitlabProjectID: f.link.ID, Name: "demo", FullName: "group/demo"})
	require.NoError(t, err)

	payload := mrPayload(mrPayloadOpts{IID: 5, Title: "Add feature", SourceBranch: "feature-5", TargetBranch: "main"})
	deliveryUUID := uuid.NewString()
	first, err := f.q.CreateWebhookEvent(ctx, db.CreateWebhookEventParams{
		LinkedGitlabProjectID: f.link.ID, DeliveryUuid: deliveryUUID, EventName: "Merge Request Hook",
		ObjectKind: "merge_request", Payload: payload, Status: "pending",
	})
	require.NoError(t, err)
	// A redelivery of the exact same event carries the same delivery_uuid;
	// webhook_events' UNIQUE (linked_gitlab_project_id, delivery_uuid)
	// constraint means CreateWebhookEvent is itself the no-op (ON CONFLICT
	// DO NOTHING), so only one row — and hence only one apply — ever exists
	// to process.
	_, err = f.q.CreateWebhookEvent(ctx, db.CreateWebhookEventParams{
		LinkedGitlabProjectID: f.link.ID, DeliveryUuid: deliveryUUID, EventName: "Merge Request Hook",
		ObjectKind: "merge_request", Payload: payload, Status: "pending",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.False(t, claimed, "the redelivery must never have been recorded as a second row")

	got, ok := f.q.GetWebhookEvent(first.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status)
}

func pipelinePayload(pipelineID int64, status string, mrIID int64) []byte {
	body := map[string]any{
		"object_kind": "pipeline",
		"object_attributes": map[string]any{
			"id":     pipelineID,
			"status": status,
		},
	}
	if mrIID != 0 {
		body["merge_request"] = map[string]any{"iid": mrIID}
	}
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return b
}

func TestProcessNext_PipelineForKnownMergeRequest_UpdatesStatus(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()
	repo, err := f.q.CreateRepository(ctx, db.CreateRepositoryParams{LinkedGitlabProjectID: f.link.ID, Name: "demo", FullName: "group/demo"})
	require.NoError(t, err)
	mr, err := f.q.CreateMergeRequest(ctx, db.CreateMergeRequestParams{
		RepositoryID: repo.ID, GitlabMergeRequestID: 1005, Number: 5, Title: "Add feature", State: "opened",
	})
	require.NoError(t, err)

	event := f.seedEvent(t, "pipeline", "Pipeline Hook", pipelinePayload(900, "success", int64(mr.Number)))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "processed", got.Status)

	updated, err := f.q.GetMergeRequestByGitlabMergeRequestID(ctx, 1005)
	require.NoError(t, err)
	assert.Equal(t, "success", updated.PipelineStatus)
	assert.EqualValues(t, 900, updated.PipelineID.Int64)
}

func TestProcessNext_PipelineWithoutMergeRequest_Skipped(t *testing.T) {
	f := newFixture(t, linkedproject.ScopeAll, nil)
	ctx := context.Background()

	event := f.seedEvent(t, "pipeline", "Pipeline Hook", pipelinePayload(900, "success", 0))

	claimed, err := f.svc.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, ok := f.q.GetWebhookEvent(event.ID)
	require.True(t, ok)
	assert.Equal(t, "skipped", got.Status)
	assert.Equal(t, webhookapply.SkipReasonOutOfScope, got.SkipReason)
}
