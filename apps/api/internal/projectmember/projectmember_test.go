package projectmember_test

import (
	"context"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/project"
	"github.com/flowlens/api/internal/projectmember"
	"github.com/flowlens/api/internal/user"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(q *dbtest.FakeQuerier) *projectmember.Service {
	return projectmember.NewService(q, project.NewService(q), user.NewService(q))
}

func TestService_List_ReturnsMembersOldestFirst(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	member := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")
	ctx := context.Background()

	members, err := svc.List(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, owner.ID, members[0].UserID)
	assert.Equal(t, "owner", members[0].Role)
	assert.Equal(t, member.ID, members[1].UserID)
	assert.Equal(t, "hubot", members[1].Username)
	assert.Equal(t, "member", members[1].Role)
}

func TestService_List_ReturnsForbiddenForNonOwner(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	viewer := q.SeedUser("viewer", "viewer@example.com")
	q.SeedProjectMember(p.ID, viewer.ID, "viewer")
	member := q.SeedUser("member", "member@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")
	ctx := context.Background()

	_, err := svc.List(ctx, viewer.ID, p.ID)
	assert.ErrorIs(t, err, projectmember.ErrForbidden, "viewer must be rejected")

	_, err = svc.List(ctx, member.ID, p.ID)
	assert.ErrorIs(t, err, projectmember.ErrForbidden, "member must be rejected")
}

func TestService_List_ReturnsNotFoundForForeignOrMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	other := q.SeedUser("hubot", "hubot@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	ctx := context.Background()

	_, err := svc.List(ctx, other.ID, p.ID)
	assert.ErrorIs(t, err, projectmember.ErrNotFound)

	_, err = svc.List(ctx, owner.ID, uuid.New())
	assert.ErrorIs(t, err, projectmember.ErrNotFound)
}

func TestService_Add(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	invitee := q.SeedUser("hubot", "hubot@example.com")
	ctx := context.Background()

	t.Run("by username", func(t *testing.T) {
		m, err := svc.Add(ctx, owner.ID, p.ID, "hubot", "member")
		require.NoError(t, err)
		assert.Equal(t, invitee.ID, m.UserID)
		assert.Equal(t, "member", m.Role)
		assert.Equal(t, invitee.Username, m.Username)
		assert.Equal(t, invitee.DisplayName, m.DisplayName)
	})

	q2 := dbtest.New()
	svc2 := newService(q2)
	owner2 := q2.SeedUser("octocat", "octocat@example.com")
	p2 := q2.SeedProject(owner2.ID, "Alpha")
	invitee2 := q2.SeedUser("hubot2", "hubot2@example.com")
	t.Run("by email", func(t *testing.T) {
		m, err := svc2.Add(ctx, owner2.ID, p2.ID, "hubot2@example.com", "viewer")
		require.NoError(t, err)
		assert.Equal(t, invitee2.ID, m.UserID)
		assert.Equal(t, "viewer", m.Role)
	})
}

func TestService_Add_ReturnsUserNotFoundForUnknownIdentifier(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	ctx := context.Background()

	_, err := svc.Add(ctx, owner.ID, p.ID, "nobody", "member")
	assert.ErrorIs(t, err, projectmember.ErrUserNotFound)
}

func TestService_Add_ReturnsAlreadyMemberForExistingMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	q.SeedUser("hubot", "hubot@example.com")
	ctx := context.Background()

	_, err := svc.Add(ctx, owner.ID, p.ID, "hubot", "member")
	require.NoError(t, err)

	_, err = svc.Add(ctx, owner.ID, p.ID, "hubot", "viewer")
	assert.ErrorIs(t, err, projectmember.ErrAlreadyMember)
}

func TestService_Add_ValidatesRole(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	q.SeedUser("hubot", "hubot@example.com")
	ctx := context.Background()

	_, err := svc.Add(ctx, owner.ID, p.ID, "hubot", "admin")
	assert.ErrorIs(t, err, projectmember.ErrInvalidRole)
}

func TestService_Add_ReturnsForbiddenForNonOwner(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	member := q.SeedUser("member", "member@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")
	q.SeedUser("hubot", "hubot@example.com")
	ctx := context.Background()

	_, err := svc.Add(ctx, member.ID, p.ID, "hubot", "viewer")
	assert.ErrorIs(t, err, projectmember.ErrForbidden)
}

func TestService_UpdateRole(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, target.ID, "viewer")
	ctx := context.Background()

	m, err := svc.UpdateRole(ctx, owner.ID, p.ID, target.ID, "member")
	require.NoError(t, err)
	assert.Equal(t, "member", m.Role)
}

func TestService_UpdateRole_ReturnsSelfRoleForOwnRoleChange(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	ctx := context.Background()

	_, err := svc.UpdateRole(ctx, owner.ID, p.ID, owner.ID, "member")
	assert.ErrorIs(t, err, projectmember.ErrSelfRole)
}

func TestService_UpdateRole_ReturnsLastOwnerForDemotingTheDesignatedOwner(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	otherOwner := q.SeedUser("otherowner", "otherowner@example.com")
	q.SeedProjectMember(p.ID, otherOwner.ID, "owner")
	ctx := context.Background()

	// otherOwner (a caller with owner role, but not the project's designated
	// owner_user_id) tries to demote the designated owner: rejected.
	_, err := svc.UpdateRole(ctx, otherOwner.ID, p.ID, owner.ID, "member")
	assert.ErrorIs(t, err, projectmember.ErrLastOwner)
}

func TestService_UpdateRole_ValidatesRole(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, target.ID, "viewer")
	ctx := context.Background()

	_, err := svc.UpdateRole(ctx, owner.ID, p.ID, target.ID, "admin")
	assert.ErrorIs(t, err, projectmember.ErrInvalidRole)
}

func TestService_UpdateRole_ReturnsNotFoundForNonMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	stranger := q.SeedUser("stranger", "stranger@example.com")
	ctx := context.Background()

	_, err := svc.UpdateRole(ctx, owner.ID, p.ID, stranger.ID, "member")
	assert.ErrorIs(t, err, projectmember.ErrNotFound)
}

func TestService_UpdateRole_ReturnsForbiddenForNonOwner(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	viewer := q.SeedUser("viewer", "viewer@example.com")
	q.SeedProjectMember(p.ID, viewer.ID, "viewer")
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, target.ID, "viewer")
	ctx := context.Background()

	_, err := svc.UpdateRole(ctx, viewer.ID, p.ID, target.ID, "member")
	assert.ErrorIs(t, err, projectmember.ErrForbidden)
}

func TestService_Remove(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, target.ID, "viewer")
	ctx := context.Background()

	require.NoError(t, svc.Remove(ctx, owner.ID, p.ID, target.ID))

	members, err := svc.List(ctx, owner.ID, p.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, owner.ID, members[0].UserID)
}

func TestService_Remove_ReturnsLastOwnerForTheDesignatedOwner(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	otherOwner := q.SeedUser("otherowner", "otherowner@example.com")
	q.SeedProjectMember(p.ID, otherOwner.ID, "owner")
	ctx := context.Background()

	err := svc.Remove(ctx, otherOwner.ID, p.ID, owner.ID)
	assert.ErrorIs(t, err, projectmember.ErrLastOwner)
}

func TestService_Remove_ReturnsNotFoundForNonMember(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	stranger := q.SeedUser("stranger", "stranger@example.com")
	ctx := context.Background()

	err := svc.Remove(ctx, owner.ID, p.ID, stranger.ID)
	assert.ErrorIs(t, err, projectmember.ErrNotFound)
}

func TestService_Remove_ReturnsForbiddenForNonOwner(t *testing.T) {
	q := dbtest.New()
	svc := newService(q)
	owner := q.SeedUser("octocat", "octocat@example.com")
	p := q.SeedProject(owner.ID, "Alpha")
	member := q.SeedUser("member", "member@example.com")
	q.SeedProjectMember(p.ID, member.ID, "member")
	target := q.SeedUser("hubot", "hubot@example.com")
	q.SeedProjectMember(p.ID, target.ID, "viewer")
	ctx := context.Background()

	err := svc.Remove(ctx, member.ID, p.ID, target.ID)
	assert.ErrorIs(t, err, projectmember.ErrForbidden)
}
