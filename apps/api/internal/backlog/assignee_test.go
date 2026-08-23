package backlog_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/optional"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A backlog's assignee is the same field a task's is, minus the GitLab bridge:
// there is no GitLab counterpart for a backlog to mirror onto.
func TestService_Create_AssignsToProjectMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")

	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", AssigneeUserID: &member})
	require.NoError(t, err)
	require.NotNil(t, created.AssigneeUserID)
	assert.Equal(t, member, *created.AssigneeUserID)
	assert.Equal(t, "hubot", created.AssigneeUsername)
}

func TestService_Assignee_RejectsNonMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	outsider := q.SeedUser("stranger", "stranger@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	_, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", AssigneeUserID: &outsider})
	assert.ErrorIs(t, err, backlog.ErrAssigneeNotMember)

	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:           "Sprint 1",
		AssigneeUserID: optional.Present(&outsider),
	}, backlog.ActorKindUser)
	assert.ErrorIs(t, err, backlog.ErrAssigneeNotMember)
}

// Renaming a backlog must not silently unassign it — the reason AssigneeUserID
// is Optional rather than a plain pointer on UpdateParams.
func TestService_Update_AbsentAssignee_KeepsCurrent(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	created, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", AssigneeUserID: &member})
	require.NoError(t, err)

	renamed, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{Name: "Sprint 2"}, backlog.ActorKindUser)
	require.NoError(t, err)
	require.NotNil(t, renamed.AssigneeUserID)
	assert.Equal(t, member, *renamed.AssigneeUserID)
	assert.Equal(t, "hubot", renamed.AssigneeUsername)

	unassigned, err := svc.Update(ctx, owner, created.ID, backlog.UpdateParams{
		Name:           "Sprint 2",
		AssigneeUserID: optional.Present[*uuid.UUID](nil),
	}, backlog.ActorKindUser)
	require.NoError(t, err)
	assert.Nil(t, unassigned.AssigneeUserID)
	assert.Empty(t, unassigned.AssigneeUsername)
}

func TestService_List_FiltersByAssignee(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	mine, err := svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 1", AssigneeUserID: &member})
	require.NoError(t, err)
	_, err = svc.Create(ctx, owner, p.ID, backlog.CreateParams{Name: "Sprint 2"})
	require.NoError(t, err)

	assigned, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{AssigneeUserID: &member})
	require.NoError(t, err)
	require.Len(t, assigned, 1)
	assert.Equal(t, mine.ID, assigned[0].ID)
	assert.Equal(t, "hubot", assigned[0].AssigneeUsername)

	unassigned, err := svc.List(ctx, owner, p.ID, backlog.ListFilter{AssigneeUnassigned: true})
	require.NoError(t, err)
	require.Len(t, unassigned, 1)
	assert.Equal(t, "Sprint 2", unassigned[0].Name)
}
