package task_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bridge's whole contract in one table: what an explicitly-set FlowLens
// assignee does to the two GitLab assignee columns. Every case runs against a
// project that *has* a GitLab connection and a linked project, since that is
// the only situation where the GitLab columns are interesting.
func TestService_Assignee_BridgesToGitlabOnlyWhenIdentityRegistered(t *testing.T) {
	tests := []struct {
		name              string
		registerIdentity  bool
		wantGitlabUserID  *int64
		wantGitlabUsersnm string
	}{
		{
			name:              "member with a registered identity mirrors onto the issue",
			registerIdentity:  true,
			wantGitlabUserID:  int64Ptr(77),
			wantGitlabUsersnm: "hubot-gl",
		},
		{
			// Not an error: plenty of members never register one. The task is
			// theirs inside FlowLens, and the GitLab assignee is cleared rather
			// than left pointing at whoever the connection's token belongs to.
			name:             "member with no identity is assigned in FlowLens only",
			registerIdentity: false,
			wantGitlabUserID: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := newService(q)
			ctx := context.Background()
			owner := q.SeedUser("octocat", "octocat@example.com").ID
			member := q.SeedUser("hubot", "hubot@example.com").ID
			p := q.SeedProject(owner, "Alpha")
			q.SeedProjectMember(p.ID, member, "member")
			conn := q.SeedGitlabConnection(p.ID, []byte("token"))
			seedLinkedGitlabProject(t, q, conn.ID)
			if tt.registerIdentity {
				q.SeedUserGitlabIdentity(member, conn.BaseUrl, 77, "hubot-gl")
			}

			created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
				Title:          "Fix bug",
				AssigneeUserID: &member,
			})
			require.NoError(t, err)
			require.NotNil(t, created.AssigneeUserID)
			assert.Equal(t, member, *created.AssigneeUserID)
			assert.Equal(t, "hubot", created.AssigneeUsername)
			assert.Equal(t, tt.wantGitlabUserID, created.AssigneeGitlabUserID)
			assert.Equal(t, tt.wantGitlabUsersnm, created.AssigneeGitlabUsername)
		})
	}
}

func TestService_Assignee_RejectsNonMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	outsider := q.SeedUser("stranger", "stranger@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug", AssigneeUserID: &outsider})
	assert.ErrorIs(t, err, task.ErrAssigneeNotMember)

	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, owner, created.ID, task.UpdateParams{
		AssigneeUserID: task.Present(&outsider),
	}, task.ActorKindUser)
	assert.ErrorIs(t, err, task.ErrAssigneeNotMember)
}

// The trap the 000031 migration's comment calls out: assignee_user_id is
// app-only, so changing it must not enqueue an issue.update on its own — only
// the GitLab columns the bridge writes can do that.
func TestService_Update_AssigneeWithoutIdentity_EnqueuesNoRedundantSync(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	// No GitLab connection at all: the task is purely local, so nothing can
	// ever be enqueued for it and the assignee is a FlowLens fact only.
	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug"})
	require.NoError(t, err)

	before := len(q.SyncJobsForTask(created.ID))
	updated, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{
		AssigneeUserID: task.Present(&member),
	}, task.ActorKindUser)
	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeUserID)
	assert.Equal(t, member, *updated.AssigneeUserID)
	assert.Len(t, q.SyncJobsForTask(created.ID), before, "an app-only field must not enqueue a GitLab sync")
}

// An absent assigneeUserId must leave *both* axes alone — this is what stops a
// PATCH of some unrelated field from reassigning the GitLab issue.
func TestService_Update_AbsentAssignee_LeavesBothAxesAlone(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	conn := q.SeedGitlabConnection(p.ID, []byte("token"))
	seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedUserGitlabIdentity(member, conn.BaseUrl, 77, "hubot-gl")

	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug", AssigneeUserID: &member})
	require.NoError(t, err)
	require.Equal(t, int64(77), *created.AssigneeGitlabUserID)

	updated, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{
		Title: task.Present("Fix bug, harder"),
	}, task.ActorKindUser)
	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeUserID)
	assert.Equal(t, member, *updated.AssigneeUserID)
	require.NotNil(t, updated.AssigneeGitlabUserID)
	assert.Equal(t, int64(77), *updated.AssigneeGitlabUserID)
}

// Unassigning clears the GitLab assignee too: a task nobody owns showing an
// assignee on the issue is the misleading outcome.
func TestService_Update_UnassigningClearsBothAxes(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	conn := q.SeedGitlabConnection(p.ID, []byte("token"))
	seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedUserGitlabIdentity(member, conn.BaseUrl, 77, "hubot-gl")

	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug", AssigneeUserID: &member})
	require.NoError(t, err)

	updated, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{
		AssigneeUserID: task.Present[*uuid.UUID](nil),
	}, task.ActorKindUser)
	require.NoError(t, err)
	assert.Nil(t, updated.AssigneeUserID)
	assert.Empty(t, updated.AssigneeUsername)
	assert.Nil(t, updated.AssigneeGitlabUserID)
	assert.Empty(t, updated.AssigneeGitlabUsername)
}

// An explicit assigneeGitlabUserId in the same request wins over the bridge —
// the path that keeps the GitLab-members picker working for someone with no
// FlowLens account.
func TestService_Update_ExplicitGitlabAssigneeOverridesBridge(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	conn := q.SeedGitlabConnection(p.ID, []byte("token"))
	seedLinkedGitlabProject(t, q, conn.ID)
	q.SeedUserGitlabIdentity(member, conn.BaseUrl, 77, "hubot-gl")

	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug"})
	require.NoError(t, err)

	updated, err := svc.Update(ctx, owner, created.ID, task.UpdateParams{
		AssigneeUserID:         task.Present(&member),
		AssigneeGitlabUserID:   task.Present(int64Ptr(99)),
		AssigneeGitlabUsername: task.Present("someone-else"),
	}, task.ActorKindUser)
	require.NoError(t, err)
	require.NotNil(t, updated.AssigneeUserID)
	assert.Equal(t, member, *updated.AssigneeUserID)
	require.NotNil(t, updated.AssigneeGitlabUserID)
	assert.Equal(t, int64(99), *updated.AssigneeGitlabUserID)
	assert.Equal(t, "someone-else", updated.AssigneeGitlabUsername)
}

// ?assignee=<user> matches on either axis, which is what makes a task synced
// in from GitLab (gitlab column only) and a purely local one (FlowLens column
// only) both show up for the same person.
func TestService_List_FiltersByAssigneeAcrossBothAxes(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	conn := q.SeedGitlabConnection(p.ID, []byte("token"))
	q.SeedUserGitlabIdentity(member, conn.BaseUrl, 77, "hubot-gl")

	flowlensOnly, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Local", AssigneeUserID: &member})
	require.NoError(t, err)
	gitlabOnly, err := svc.Create(ctx, owner, p.ID, task.CreateParams{
		Title:                  "From GitLab",
		AssigneeGitlabUserID:   int64Ptr(77),
		AssigneeGitlabUsername: "hubot-gl",
	})
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Nobody's"})
	require.NoError(t, err)

	matchedPage, err := svc.List(ctx, owner, p.ID, task.ListFilter{AssigneeUserID: &member})
	matched := matchedPage.Tasks
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]uuid.UUID{flowlensOnly.ID, gitlabOnly.ID},
		[]uuid.UUID{matched[0].ID, matched[1].ID})

	unassignedPage, err := svc.List(ctx, owner, p.ID, task.ListFilter{AssigneeUnassigned: true})
	unassigned := unassignedPage.Tasks
	require.NoError(t, err)
	require.Len(t, unassigned, 1)
	assert.Equal(t, "Nobody's", unassigned[0].Title)
}

// The assignee's name is resolved from users on read, not stored, so a rename
// is picked up without a backfill.
func TestService_Get_ResolvesAssigneeNameFromUsers(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")

	created, err := svc.Create(ctx, owner, p.ID, task.CreateParams{Title: "Fix bug", AssigneeUserID: &member})
	require.NoError(t, err)

	got, err := svc.Get(ctx, owner, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "hubot", got.AssigneeUsername)

	// And it reaches the AI-facing context, so an agent knows whose work it is.
	c, err := svc.Context(ctx, owner, created.ID)
	require.NoError(t, err)
	require.NotNil(t, c.AssigneeUserID)
	assert.Equal(t, member, *c.AssigneeUserID)
	assert.Equal(t, "hubot", c.AssigneeUsername)
}

func int64Ptr(v int64) *int64 { return &v }
