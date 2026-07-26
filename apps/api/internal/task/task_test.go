package task_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *task.Service {
	projects := project.NewService(q)
	backlogs := backlog.NewService(q, projects)
	return task.NewService(q, projects, backlogs)
}

func TestService_Create_ValidatesTitle(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  Fix bug  ", "Fix bug", nil},
		{"rejects empty after trim", "   ", "", task.ErrInvalidTitle},
		{"rejects too long", strings.Repeat("a", 256), "", task.ErrInvalidTitle},
		{"accepts max length", strings.Repeat("a", 255), strings.Repeat("a", 255), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			p := q.SeedProject(owner, "Alpha")

			got, err := svc.Create(context.Background(), owner, p.ID, task.CreateParams{Title: tt.input})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Title)
			assert.Equal(t, task.StatusOpen, got.Status)
			assert.Nil(t, got.BacklogID)
			assert.Nil(t, got.Gitlab)
		})
	}
}

func TestService_Create_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Create(context.Background(), other, p.ID, task.CreateParams{Title: "Fix bug"})
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_Create_RejectsBacklogFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(owner, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")

	_, err := svc.Create(context.Background(), owner, p.ID, task.CreateParams{
		Title:     "Fix bug",
		BacklogID: &foreignBacklog.ID,
	})
	assert.ErrorIs(t, err, task.ErrBacklogNotInProject)
}

func TestService_AssignBacklog_RejectsBacklogFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	otherProject := q.SeedProject(owner, "Beta")
	foreignBacklog := q.SeedBacklog(otherProject.ID, "Sprint 1")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.AssignBacklog(context.Background(), owner, tsk.ID, &foreignBacklog.ID)
	assert.ErrorIs(t, err, task.ErrBacklogNotInProject)

	still, err := svc.Get(context.Background(), owner, tsk.ID)
	require.NoError(t, err)
	assert.Nil(t, still.BacklogID)
}

func TestService_AssignBacklog_AssignsWithinSameProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	got, err := svc.AssignBacklog(context.Background(), owner, tsk.ID, &b.ID)
	require.NoError(t, err)
	require.NotNil(t, got.BacklogID)
	assert.Equal(t, b.ID, *got.BacklogID)
}

func TestService_AssignBacklog_NilReturnsTaskToUnfiled(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b := q.SeedBacklog(p.ID, "Sprint 1")
	tsk := q.SeedTaskInBacklog(p.ID, b.ID, owner, "Fix bug")

	got, err := svc.AssignBacklog(context.Background(), owner, tsk.ID, nil)
	require.NoError(t, err)
	assert.Nil(t, got.BacklogID)
}

func TestService_List_FiltersByBacklogAndStatus(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	b1 := q.SeedBacklog(p.ID, "Sprint 1")
	b2 := q.SeedBacklog(p.ID, "Sprint 2")

	inB1 := q.SeedTaskInBacklog(p.ID, b1.ID, owner, "In sprint 1")
	inB2 := q.SeedTaskInBacklog(p.ID, b2.ID, owner, "In sprint 2")
	unfiled := q.SeedTask(p.ID, owner, "Unfiled")

	closedUnfiled, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Closed unfiled"})
	require.NoError(t, err)
	_, err = svc.Close(ctx, owner, closedUnfiled.ID)
	require.NoError(t, err)

	tests := []struct {
		name   string
		filter task.ListFilter
		want   []uuid.UUID
	}{
		{"no filter returns everything", task.ListFilter{}, []uuid.UUID{inB1.ID, inB2.ID, unfiled.ID, closedUnfiled.ID}},
		{"backlog_id=unassigned returns only unfiled tasks", task.ListFilter{Unassigned: true}, []uuid.UUID{unfiled.ID, closedUnfiled.ID}},
		{"backlog_id=<id> scopes to one backlog", task.ListFilter{BacklogID: &b1.ID}, []uuid.UUID{inB1.ID}},
		{"status=open excludes closed tasks", task.ListFilter{Status: task.StatusOpen}, []uuid.UUID{inB1.ID, inB2.ID, unfiled.ID}},
		{"unassigned+status=closed combine", task.ListFilter{Unassigned: true, Status: task.StatusClosed}, []uuid.UUID{closedUnfiled.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.List(ctx, owner, p.ID, tt.filter)
			require.NoError(t, err)
			ids := make([]uuid.UUID, len(got))
			for i, tk := range got {
				ids[i] = tk.ID
			}
			assert.ElementsMatch(t, tt.want, ids)
		})
	}
}

func TestService_List_ReturnsNotFoundForForeignProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.List(context.Background(), other, p.ID, task.ListFilter{})
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_Close_IsIdempotent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	first, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusClosed, first.Status)
	require.NotNil(t, first.ClosedAt)

	second, err := svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.NotNil(t, second.ClosedAt)
	assert.Equal(t, first.ClosedAt.UnixNano(), second.ClosedAt.UnixNano())
}

func TestService_Reopen_IsIdempotent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	// Reopening an already-open task is a no-op.
	opened, err := svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, opened.Status)
	assert.Nil(t, opened.ClosedAt)

	_, err = svc.Close(ctx, owner, tsk.ID)
	require.NoError(t, err)

	first, err := svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, first.Status)
	assert.Nil(t, first.ClosedAt)

	second, err := svc.Reopen(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusOpen, second.Status)
	assert.Nil(t, second.ClosedAt)
}

func TestService_Delete_RemovesTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	require.NoError(t, svc.Delete(context.Background(), owner, tsk.ID))

	_, err := svc.Get(context.Background(), owner, tsk.ID)
	assert.ErrorIs(t, err, task.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForMissingTask(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	assert.ErrorIs(t, svc.Delete(context.Background(), owner, uuid.New()), task.ErrNotFound)
}

func TestService_UpsertAIContext_CreatesThenUpdates(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	created, err := svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Given/When/Then",
		AIContext:          "Legacy payments module",
		AllowedScope:       "internal/payments/**",
		ForbiddenScope:     "internal/auth/**",
	})
	require.NoError(t, err)
	assert.Equal(t, "Given/When/Then", created.AcceptanceCriteria)
	assert.Equal(t, "Legacy payments module", created.AIContext)
	assert.Equal(t, "internal/payments/**", created.AllowedScope)
	assert.Equal(t, "internal/auth/**", created.ForbiddenScope)
	require.NotNil(t, created.UpdatedAt)

	got, err := svc.GetAIContext(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	updated, err := svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Updated criteria",
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated criteria", updated.AcceptanceCriteria)
	// Fields omitted from the second call overwrite, they don't merge.
	assert.Equal(t, "", updated.AIContext)
	assert.Equal(t, "", updated.AllowedScope)
	assert.Equal(t, "", updated.ForbiddenScope)

	got, err = svc.GetAIContext(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, updated, got)
}

func TestService_GetAIContext_ReturnsZeroValueWhenNeverSet(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	got, err := svc.GetAIContext(context.Background(), owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.AIContext{}, got)
}

func TestService_UpsertAIContext_RejectsFieldOverLengthLimit(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.UpsertAIContext(context.Background(), owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: strings.Repeat("a", 20001),
	})
	assert.ErrorIs(t, err, task.ErrAIContextFieldTooLong)
}

func TestService_UpsertAIContext_ForeignTaskGetsNotFound(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.UpsertAIContext(context.Background(), other, tsk.ID, task.AIContextParams{AcceptanceCriteria: "x"})
	assert.ErrorIs(t, err, task.ErrNotFound)

	got, err := svc.GetAIContext(context.Background(), owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.AIContext{}, got, "a rejected write from a non-owner must not land")
}

// This is the invariant the sync feature depends on: an AI-context-only edit
// must never look like a task edit, or it would spuriously enqueue a sync
// job once the outbox worker ships (docs/plans/issue-sync.md).
func TestService_UpsertAIContext_DoesNotChangeTaskUpdatedAt(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	before, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)

	_, err = svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "Given/When/Then",
		AIContext:          "Legacy payments module",
		AllowedScope:       "internal/payments/**",
		ForbiddenScope:     "internal/auth/**",
	})
	require.NoError(t, err)

	after, err := svc.Get(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Equal(t, before.UpdatedAt.UnixNano(), after.UpdatedAt.UnixNano())
}

// BuildGitlabIssuePayload must never reference task_ai_contexts fields, since
// they are app-only and must never be sent to GitLab (docs/plans/issue-sync.md,
// "Why the task is split across three tables"). This is pinned as a
// prerequisite for the GitLab issue sync feature.
func TestBuildGitlabIssuePayload_NeverReferencesAIContextFields(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	ctx := context.Background()

	tk, err := svc.Update(ctx, owner, tsk.ID, task.UpdateParams{
		Title:       "Fix bug",
		Description: "does not mention acceptance criteria",
		Labels:      []string{"bug"},
	})
	require.NoError(t, err)

	_, err = svc.UpsertAIContext(ctx, owner, tsk.ID, task.AIContextParams{
		AcceptanceCriteria: "SECRET_ACCEPTANCE_CRITERIA",
		AIContext:          "SECRET_AI_CONTEXT",
		AllowedScope:       "SECRET_ALLOWED_SCOPE",
		ForbiddenScope:     "SECRET_FORBIDDEN_SCOPE",
	})
	require.NoError(t, err)

	payload := task.BuildGitlabIssuePayload(tk)

	body, err := json.Marshal(payload)
	require.NoError(t, err)
	for _, secret := range []string{
		"SECRET_ACCEPTANCE_CRITERIA", "SECRET_AI_CONTEXT", "SECRET_ALLOWED_SCOPE", "SECRET_FORBIDDEN_SCOPE",
	} {
		assert.NotContains(t, string(body), secret)
	}
	assert.Equal(t, "Fix bug", payload.Title)
	assert.Equal(t, "does not mention acceptance criteria", payload.Description)
	assert.Equal(t, []string{"bug"}, payload.Labels)
}

// Ownership is enforced through the parent project, so a non-owner is told
// the task does not exist for reads and is refused for writes.
func TestService_ScopesEveryOperationToProjectOwner(t *testing.T) {
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		tsk := q.SeedTask(p.ID, owner, "Fix bug")

		_, err := svc.Get(ctx, other, tsk.ID)
		assert.ErrorIs(t, err, task.ErrNotFound)
	})

	t.Run("update leaves the task untouched", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		tsk := q.SeedTask(p.ID, owner, "Fix bug")

		_, err := svc.Update(ctx, other, tsk.ID, task.UpdateParams{Title: "Hijacked"})
		require.ErrorIs(t, err, task.ErrNotFound)

		still, err := svc.Get(ctx, owner, tsk.ID)
		require.NoError(t, err)
		assert.Equal(t, "Fix bug", still.Title)
	})

	t.Run("delete leaves the task in place", func(t *testing.T) {
		q := dbtest.New()
		svc := newService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")
		tsk := q.SeedTask(p.ID, owner, "Fix bug")

		err := svc.Delete(ctx, other, tsk.ID)
		require.ErrorIs(t, err, task.ErrNotFound)

		_, err = svc.Get(ctx, owner, tsk.ID)
		assert.NoError(t, err)
	})
}
