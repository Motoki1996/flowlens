package taskcomment_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/backlog"
	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/task"
	"github.com/flowlens/api/internal/taskcomment"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) (*taskcomment.Service, *task.Service) {
	projects := project.NewService(q)
	backlogs := backlog.NewService(q, projects)
	tasks := task.NewService(q, dbtest.FakeTxRunner{Q: q}, projects, backlogs)
	return taskcomment.NewService(q, projects, tasks), tasks
}

func TestService_Create_SetsAuthorKindUser(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	c, err := svc.Create(ctx, owner, tsk.ID, "Started looking into this.")
	require.NoError(t, err)
	assert.Equal(t, taskcomment.AuthorKindUser, c.AuthorKind)
	require.NotNil(t, c.AuthorUserID)
	assert.Equal(t, owner, *c.AuthorUserID)
	assert.Nil(t, c.AuthorTokenID)
	assert.Equal(t, "Started looking into this.", c.Body)
}

func TestService_CreateForProject_SetsAuthorKindAgent(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	tokenID := uuid.New()

	c, err := svc.CreateForProject(ctx, p.ID, tokenID, tsk.ID, "Pushed a fix in MR !12.")
	require.NoError(t, err)
	assert.Equal(t, taskcomment.AuthorKindAgent, c.AuthorKind)
	require.NotNil(t, c.AuthorTokenID)
	assert.Equal(t, tokenID, *c.AuthorTokenID)
	assert.Nil(t, c.AuthorUserID)
}

func TestService_CreateForProject_RejectsTaskFromAnotherProject(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p1 := q.SeedProject(owner, "Alpha")
	p2 := q.SeedProject(owner, "Beta")
	tsk := q.SeedTask(p2.ID, owner, "Other project's task")

	_, err := svc.CreateForProject(ctx, p1.ID, uuid.New(), tsk.ID, "Should not land")
	assert.ErrorIs(t, err, taskcomment.ErrNotFound)
}

func TestService_Create_ReturnsNotFoundForForeignProjectTask(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	intruder := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.Create(ctx, intruder, tsk.ID, "Should not land")
	assert.ErrorIs(t, err, taskcomment.ErrNotFound)
}

func TestService_Create_RejectsEmptyOrTooLongBody(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	_, err := svc.Create(ctx, owner, tsk.ID, "")
	assert.ErrorIs(t, err, taskcomment.ErrInvalidBody)

	_, err = svc.Create(ctx, owner, tsk.ID, strings.Repeat("a", 10001))
	assert.ErrorIs(t, err, taskcomment.ErrInvalidBody)
}

func TestService_List_ReturnsOldestFirst(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")

	first, err := svc.Create(ctx, owner, tsk.ID, "First")
	require.NoError(t, err)
	second, err := svc.Create(ctx, owner, tsk.ID, "Second")
	require.NoError(t, err)

	comments, err := svc.List(ctx, owner, tsk.ID)
	require.NoError(t, err)
	require.Len(t, comments, 2)
	assert.Equal(t, first.ID, comments[0].ID)
	assert.Equal(t, second.ID, comments[1].ID)
}

func TestService_ListForProject_ScopesToProject(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p1 := q.SeedProject(owner, "Alpha")
	p2 := q.SeedProject(owner, "Beta")
	tsk := q.SeedTask(p1.ID, owner, "Fix bug")
	_, err := svc.Create(ctx, owner, tsk.ID, "Comment")
	require.NoError(t, err)

	_, err = svc.ListForProject(ctx, p2.ID, tsk.ID)
	assert.ErrorIs(t, err, taskcomment.ErrNotFound)

	comments, err := svc.ListForProject(ctx, p1.ID, tsk.ID)
	require.NoError(t, err)
	assert.Len(t, comments, 1)
}

func TestListRecent_CapsToRecentLimit(t *testing.T) {
	all := make([]taskcomment.TaskComment, taskcomment.RecentLimit+5)
	for i := range all {
		all[i] = taskcomment.TaskComment{ID: uuid.New()}
	}

	recent := taskcomment.ListRecent(all)
	require.Len(t, recent, taskcomment.RecentLimit)
	assert.Equal(t, all[5:], recent, "must keep the most recently appended entries")
}

func TestService_Delete_OwnCommentSucceeds(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	c, err := svc.Create(ctx, owner, tsk.ID, "Comment")
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, owner, c.ID))

	comments, err := svc.List(ctx, owner, tsk.ID)
	require.NoError(t, err)
	assert.Empty(t, comments)
}

func TestService_Delete_OtherUsersCommentIsForbidden(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	teammate := q.SeedUser("hubot", "hubot@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, teammate, "member")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	c, err := svc.Create(ctx, owner, tsk.ID, "Comment")
	require.NoError(t, err)

	err = svc.Delete(ctx, teammate, c.ID)
	assert.ErrorIs(t, err, taskcomment.ErrForbidden)
}

func TestService_DeleteForToken_OwnCommentSucceeds(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	tokenID := uuid.New()
	c, err := svc.CreateForProject(ctx, p.ID, tokenID, tsk.ID, "Done")
	require.NoError(t, err)

	require.NoError(t, svc.DeleteForToken(ctx, tokenID, c.ID))
}

func TestService_DeleteForToken_AnotherTokensCommentIsForbidden(t *testing.T) {
	q := dbtest.New()
	svc, _ := newService(q)
	ctx := context.Background()
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	tsk := q.SeedTask(p.ID, owner, "Fix bug")
	c, err := svc.CreateForProject(ctx, p.ID, uuid.New(), tsk.ID, "Done")
	require.NoError(t, err)

	err = svc.DeleteForToken(ctx, uuid.New(), c.ID)
	assert.ErrorIs(t, err, taskcomment.ErrForbidden)
}
