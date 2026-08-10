package project_test

import (
	"context"
	"strings"
	"testing"

	"github.com/flowlens/api/internal/database/dbtest"
	"github.com/flowlens/api/internal/issuesync"
	"github.com/flowlens/api/internal/project"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_Create_ValidatesName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{"trims whitespace", "  My Project  ", "My Project", nil},
		{"rejects empty after trim", "   ", "", project.ErrInvalidName},
		{"rejects too long", strings.Repeat("a", 101), "", project.ErrInvalidName},
		{"accepts max length", strings.Repeat("a", 100), strings.Repeat("a", 100), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dbtest.New()
			svc := project.NewService(q)
			owner := q.SeedUser("octocat", "octocat@example.com").ID

			p, err := svc.Create(context.Background(), owner, tt.input, "")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.Name)
		})
	}
}

func TestService_Create_RejectsDuplicateNameForSameOwner(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Create(context.Background(), owner, "Alpha", "")
	require.NoError(t, err)

	_, err = svc.Create(context.Background(), owner, "Alpha", "")
	assert.ErrorIs(t, err, project.ErrNameTaken)
}

func TestService_Create_AllowsSameNameForDifferentOwners(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner1 := q.SeedUser("octocat", "octocat@example.com").ID
	owner2 := q.SeedUser("hubot", "hubot@example.com").ID

	_, err := svc.Create(context.Background(), owner1, "Alpha", "")
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), owner2, "Alpha", "")
	assert.NoError(t, err)
}

// Ownership is enforced inside every method, so a non-owner is told the
// project does not exist for reads and is refused for writes.
func TestService_ScopesEveryOperationToOwner(t *testing.T) {
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		q := dbtest.New()
		svc := project.NewService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")

		_, err := svc.Get(ctx, other, p.ID)
		assert.ErrorIs(t, err, project.ErrNotFound)
	})

	t.Run("update leaves the project untouched", func(t *testing.T) {
		q := dbtest.New()
		svc := project.NewService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")

		_, err := svc.Update(ctx, other, p.ID, "Hijacked", "")
		require.ErrorIs(t, err, project.ErrNotFound)

		still, err := svc.Get(ctx, owner, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "Alpha", still.Name)
	})

	t.Run("delete leaves the project in place", func(t *testing.T) {
		q := dbtest.New()
		svc := project.NewService(q)
		owner := q.SeedUser("octocat", "octocat@example.com").ID
		other := q.SeedUser("hubot", "hubot@example.com").ID
		p := q.SeedProject(owner, "Alpha")

		err := svc.Delete(ctx, other, p.ID)
		require.ErrorIs(t, err, project.ErrNotFound)

		_, err = svc.Get(ctx, owner, p.ID)
		assert.NoError(t, err)
	})
}

// TestService_Role_ReflectsMembership covers issue #99's core primitive:
// RoleOwner for the creator (backfilled by Create), RoleNone (not an error)
// for a user with no project_members row at all, and whatever role a
// SeedProjectMember row carries for anyone else.
func TestService_Role_ReflectsMembership(t *testing.T) {
	ctx := context.Background()
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	viewer := q.SeedUser("viewer", "viewer@example.com").ID
	stranger := q.SeedUser("stranger", "stranger@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, viewer, "viewer")

	role, err := svc.Role(ctx, owner, p.ID)
	require.NoError(t, err)
	assert.Equal(t, project.RoleOwner, role)

	role, err = svc.Role(ctx, viewer, p.ID)
	require.NoError(t, err)
	assert.Equal(t, project.RoleViewer, role)

	role, err = svc.Role(ctx, stranger, p.ID)
	require.NoError(t, err, "no membership row is a valid state, not an error")
	assert.Equal(t, project.RoleNone, role)
}

// TestService_Create_GrantsCreatorOwnerMembership confirms Create itself
// (not just SeedProject) leaves the creator able to act as owner
// immediately — the same invariant migration 000012's backfill establishes
// for pre-existing projects (docs/decisions/0010-why-project-membership.md).
func TestService_Create_GrantsCreatorOwnerMembership(t *testing.T) {
	ctx := context.Background()
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	p, err := svc.Create(ctx, owner, "Alpha", "")
	require.NoError(t, err)

	role, err := svc.Role(ctx, owner, p.ID)
	require.NoError(t, err)
	assert.Equal(t, project.RoleOwner, role)
	assert.NoError(t, svc.Authorize(ctx, owner, p.ID, project.RoleOwner))
}

// TestService_Authorize_DistinguishesForbiddenFromNotFound is issue #99's
// central completion criterion: a caller with a membership row below min
// gets ErrForbidden (they exist, their role is insufficient); a caller with
// no membership row at all gets ErrNotFound (existence is never leaked).
func TestService_Authorize_DistinguishesForbiddenFromNotFound(t *testing.T) {
	ctx := context.Background()
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	viewer := q.SeedUser("viewer", "viewer@example.com").ID
	member := q.SeedUser("member", "member@example.com").ID
	stranger := q.SeedUser("stranger", "stranger@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, viewer, "viewer")
	q.SeedProjectMember(p.ID, member, "member")

	assert.NoError(t, svc.Authorize(ctx, owner, p.ID, project.RoleOwner))
	assert.NoError(t, svc.Authorize(ctx, member, p.ID, project.RoleMember))
	assert.NoError(t, svc.Authorize(ctx, viewer, p.ID, project.RoleViewer))

	assert.ErrorIs(t, svc.Authorize(ctx, viewer, p.ID, project.RoleMember), project.ErrForbidden)
	assert.ErrorIs(t, svc.Authorize(ctx, member, p.ID, project.RoleOwner), project.ErrForbidden)

	assert.ErrorIs(t, svc.Authorize(ctx, stranger, p.ID, project.RoleViewer), project.ErrNotFound)
	assert.ErrorIs(t, svc.Authorize(ctx, stranger, p.ID, project.RoleOwner), project.ErrNotFound)
}

// TestService_Update_MemberCanWriteViewerCannot covers Update's
// member-minimum tier: a member can rename the project, a viewer gets
// ErrForbidden and the project is left untouched.
func TestService_Update_MemberCanWriteViewerCannot(t *testing.T) {
	ctx := context.Background()
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("member", "member@example.com").ID
	viewer := q.SeedUser("viewer", "viewer@example.com").ID
	p := q.SeedProject(owner, "Alpha")
	q.SeedProjectMember(p.ID, member, "member")
	q.SeedProjectMember(p.ID, viewer, "viewer")

	_, err := svc.Update(ctx, viewer, p.ID, "Hijacked", "")
	assert.ErrorIs(t, err, project.ErrForbidden)

	updated, err := svc.Update(ctx, member, p.ID, "Renamed", "")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
}

// TestService_List_ReturnsEveryProjectTheCallerIsAMemberOf covers issue
// #99's "List becomes membership-based" change: a project the caller was
// merely added to (not owns) is returned too, and a project the caller has
// no membership row for at all is not.
func TestService_List_ReturnsEveryProjectTheCallerIsAMemberOf(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	member := q.SeedUser("member", "member@example.com").ID
	owned := q.SeedProject(member, "Mine")
	shared := q.SeedProject(owner, "Shared")
	q.SeedProjectMember(shared.ID, member, "viewer")
	q.SeedProject(owner, "NotMine")

	projects, err := svc.List(context.Background(), member)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	names := []string{projects[0].Name, projects[1].Name}
	assert.ElementsMatch(t, []string{owned.Name, shared.Name}, names)
}

func TestService_Get_ReturnsNotFoundForMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	_, err := svc.Get(context.Background(), owner, uuid.New())
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_List_ScopesToOwner(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID
	q.SeedProject(owner, "Alpha")
	q.SeedProject(owner, "Beta")
	q.SeedProject(other, "Gamma")

	projects, err := svc.List(context.Background(), owner)
	require.NoError(t, err)
	assert.Len(t, projects, 2)
}

func TestService_ListFailedSync_ReturnsOnlyProjectsWithFailures(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	other := q.SeedUser("hubot", "hubot@example.com").ID

	clean := q.SeedProject(owner, "Clean")
	q.SeedTask(clean.ID, owner, "Fine")

	broken := q.SeedProject(owner, "Broken")
	failedTask := q.SeedTask(broken.ID, owner, "Broken task")
	q.SeedSyncJobForTask(failedTask.ID, broken.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	otherBroken := q.SeedProject(other, "Other's broken project")
	otherFailedTask := q.SeedTask(otherBroken.ID, other, "Not mine")
	q.SeedSyncJobForTask(otherFailedTask.ID, otherBroken.ID, issuesync.KindIssueCreate, "failed", "gitlab unreachable")

	projects, err := svc.ListFailedSync(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "Broken", projects[0].Name)
	assert.Equal(t, int64(1), projects[0].FailedSyncTaskCount)
}

func TestService_Update_RejectsDuplicateName(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	q.SeedProject(owner, "Alpha")
	beta := q.SeedProject(owner, "Beta")

	_, err := svc.Update(context.Background(), owner, beta.ID, "Alpha", "")
	assert.ErrorIs(t, err, project.ErrNameTaken)
}

func TestService_Update_ChangesNameAndDescription(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	updated, err := svc.Update(context.Background(), owner, p.ID, "Renamed", "new description")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", updated.Name)
	assert.Equal(t, "new description", updated.Description)
}

func TestService_Delete_RemovesProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID
	p := q.SeedProject(owner, "Alpha")

	require.NoError(t, svc.Delete(context.Background(), owner, p.ID))

	_, err := svc.Get(context.Background(), owner, p.ID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestService_Delete_ReturnsNotFoundForMissingProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	assert.ErrorIs(t, svc.Delete(context.Background(), owner, uuid.New()), project.ErrNotFound)
}

func TestCreate_ReturnsDomainProject(t *testing.T) {
	q := dbtest.New()
	svc := project.NewService(q)
	owner := q.SeedUser("octocat", "octocat@example.com").ID

	p, err := svc.Create(context.Background(), owner, "Alpha", "desc")
	require.NoError(t, err)

	assert.Equal(t, "Alpha", p.Name)
	assert.Equal(t, "desc", p.Description)
	assert.NotEqual(t, uuid.Nil, p.ID)
	assert.False(t, p.CreatedAt.IsZero())
}
